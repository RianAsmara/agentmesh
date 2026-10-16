package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// AgentStore adalah implementasi store.AgentStore di atas PostgreSQL.
type AgentStore struct {
	db *gorm.DB
}

// NewAgentStore membuat AgentStore baru.
func NewAgentStore(db *gorm.DB) *AgentStore {
	return &AgentStore{db: db}
}

var _ store.AgentStore = (*AgentStore)(nil)

// Create menyimpan agent beserta seluruh kapabilitasnya dalam satu
// transaksi.
//
// Transaksi diletakkan di sini, bukan di handler. Begitu handler tahu soal
// transaksi, ia butuh *gorm.DB, dan sekat yang menjaga seluruh rancangan
// ini runtuh. Sifat "semua jadi atau tidak sama sekali" adalah bagian dari
// arti "membuat agent", jadi tempatnya memang di implementasi operasi itu.
//
// Perhatikan tidak adanya pola "cek dulu, lalu insert". Dua permintaan
// bersamaan akan sama-sama lolos pengecekan, lalu sama-sama menyisipkan
// baris. Unique index (tenant_id, name) itulah yang benar-benar mencegah
// duplikat: kita coba sisipkan, lalu terjemahkan pelanggarannya. Biarkan
// database yang memutuskan perlombaan — hanya dia yang bisa.
func (s *AgentStore) Create(ctx context.Context, a *domain.Agent) error {
	// Dipotong ke presisi mikrodetik: itulah presisi maksimum kolom
	// TIMESTAMPTZ. Tanpa pemotongan ini, nilai di memori punya nanodetik
	// yang hilang begitu PostgreSQL menyimpannya, sehingga pembacaan
	// berikutnya tidak lagi sama persis dengan nilai yang baru ditulis.
	now := time.Now().UTC().Truncate(time.Microsecond)
	a.CreatedAt = now
	a.UpdatedAt = now

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m := agentFromDomain(a)
		if err := tx.Omit("Capabilities").Create(&m).Error; err != nil {
			return translateErr(err)
		}

		for i := range a.Capabilities {
			c := &a.Capabilities[i]
			if c.ID == uuid.Nil {
				c.ID = uuid.New()
			}
			c.AgentID = a.ID
			c.CreatedAt = now

			cm := capabilityFromDomain(c, a.ID)
			if err := tx.Create(&cm).Error; err != nil {
				return translateErr(err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("create agent %q: %w", a.Name, err)
	}
	return nil
}

// GetByName mengambil satu agent beserta kapabilitasnya.
//
// tenantID selalu ikut dalam klausa WHERE. Inilah yang membuat kebocoran
// antar-tenant mustahil terjadi karena lupa menyaring: tidak ada jalan di
// interface ini untuk mengambil agent tanpa menyebut tenant-nya.
func (s *AgentStore) GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Agent, error) {
	var m agentModel
	err := s.db.WithContext(ctx).
		Preload("Capabilities", func(db *gorm.DB) *gorm.DB {
			// Urutan eksplisit agar response API stabil antar permintaan.
			return db.Order("capabilities.name, capabilities.version")
		}).
		Where("tenant_id = ? AND name = ?", tenantID, name).
		First(&m).Error
	if err != nil {
		return nil, fmt.Errorf("get agent %q: %w", name, translateErr(err))
	}

	a := m.toDomain()
	return &a, nil
}

// List mengembalikan satu halaman agent, disaring sesuai filter.
func (s *AgentStore) List(ctx context.Context, tenantID uuid.UUID, f store.AgentFilter) ([]domain.Agent, string, error) {
	limit := clampLimit(f.Limit)

	q := s.db.WithContext(ctx).
		Model(&agentModel{}).
		Where("agents.tenant_id = ?", tenantID)

	if f.Status != "" {
		q = q.Where("agents.status = ?", f.Status)
	}

	if f.Capability != "" {
		// Subquery, bukan JOIN. Dengan JOIN, agent yang punya dua
		// kapabilitas cocok akan muncul dua kali dan harus dibuang dengan
		// DISTINCT — yang pada gilirannya merusak paginasi cursor karena
		// DISTINCT bekerja setelah LIMIT diterapkan. EXISTS mengembalikan
		// tepat satu baris per agent, apa pun jumlah kapabilitasnya.
		q = q.Where(`EXISTS (
			SELECT 1 FROM capabilities c
			 WHERE c.agent_id = agents.id AND c.name = ?)`, f.Capability)
	}

	if f.Cursor != "" {
		createdAt, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("list agents: %w", err)
		}
		q = q.Where("(agents.created_at, agents.id) > (?, ?)", createdAt, id)
	}

	var models []agentModel
	err := q.
		Preload("Capabilities", func(db *gorm.DB) *gorm.DB {
			return db.Order("capabilities.name, capabilities.version")
		}).
		Order("agents.created_at, agents.id").
		Limit(limit + 1).
		Find(&models).Error
	if err != nil {
		return nil, "", fmt.Errorf("list agents: %w", translateErr(err))
	}

	next := ""
	if len(models) > limit {
		models = models[:limit]
		last := models[len(models)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}

	items := make([]domain.Agent, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, next, nil
}

// Update menyimpan perubahan endpoint dan status.
//
// Hanya dua kolom itu yang ditulis. Nama agent adalah identitas di URL dan
// tidak pernah berubah; kapabilitas dikelola lewat create dan delete, bukan
// lewat update parsial — mengganti seluruh daftar kapabilitas diam-diam
// akan menjadi kejutan yang tidak diinginkan.
func (s *AgentStore) Update(ctx context.Context, a *domain.Agent) error {
	a.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)

	res := s.db.WithContext(ctx).
		Model(&agentModel{}).
		Where("tenant_id = ? AND name = ?", a.TenantID, a.Name).
		Updates(map[string]any{
			"endpoint":   a.Endpoint,
			"status":     string(a.Status),
			"updated_at": a.UpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("update agent %q: %w", a.Name, translateErr(res.Error))
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("update agent %q: %w", a.Name, domain.ErrNotFound)
	}
	return nil
}

// Delete menghapus agent. Kapabilitasnya ikut terhapus lewat ON DELETE
// CASCADE yang didefinisikan di migrasi 0001.
func (s *AgentStore) Delete(ctx context.Context, tenantID uuid.UUID, name string) error {
	res := s.db.WithContext(ctx).
		Where("tenant_id = ? AND name = ?", tenantID, name).
		Delete(&agentModel{})
	if res.Error != nil {
		return fmt.Errorf("delete agent %q: %w", name, translateErr(res.Error))
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delete agent %q: %w", name, domain.ErrNotFound)
	}
	return nil
}
