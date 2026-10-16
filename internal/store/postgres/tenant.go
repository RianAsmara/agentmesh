package postgres

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// TenantStore adalah implementasi store.TenantStore di atas PostgreSQL.
type TenantStore struct {
	db *gorm.DB
}

// NewTenantStore membuat TenantStore baru.
func NewTenantStore(db *gorm.DB) *TenantStore {
	return &TenantStore{db: db}
}

// Pernyataan waktu-kompilasi bahwa tipe ini benar-benar memenuhi kontrak.
// Kalau interface berubah, build gagal di sini, bukan di tempat pemakaian.
var _ store.TenantStore = (*TenantStore)(nil)

// Create menyimpan tenant baru.
//
// Timestamp diisi di sini, bukan dibiarkan pada DEFAULT now() milik kolom,
// supaya nilai yang dikembalikan ke pemanggil sama persis dengan yang
// tersimpan — tanpa perlu membaca ulang barisnya. Dipotong ke presisi
// mikrodetik karena itulah presisi maksimum kolom TIMESTAMPTZ; tanpa
// pemotongan ini, nilai di memori punya nanodetik yang akan hilang begitu
// PostgreSQL menyimpannya, sehingga pembacaan berikutnya tidak lagi sama
// persis dengan nilai yang baru saja ditulis.
func (s *TenantStore) Create(ctx context.Context, t *domain.Tenant) error {
	now := time.Now().UTC().Truncate(time.Microsecond)
	t.CreatedAt = now
	t.UpdatedAt = now

	m := tenantFromDomain(t)
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return fmt.Errorf("create tenant: %w", translateErr(err))
	}
	return nil
}

// GetBySlug mengambil satu tenant berdasarkan slug.
func (s *TenantStore) GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	var m tenantModel
	err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&m).Error
	if err != nil {
		return nil, fmt.Errorf("get tenant %q: %w", slug, translateErr(err))
	}
	t := m.toDomain()
	return &t, nil
}

// List mengembalikan satu halaman tenant beserta cursor halaman berikutnya.
//
// Trik paginasi di sini: ambil limit+1 baris. Kalau yang kembali memang
// limit+1, berarti masih ada halaman lagi — baris tambahan itu dibuang dan
// cursor dibuat dari baris terakhir yang benar-benar dikembalikan. Dengan
// cara ini tidak perlu COUNT(*) terpisah hanya untuk tahu ada tidaknya
// halaman berikutnya.
func (s *TenantStore) List(ctx context.Context, f store.TenantFilter) ([]domain.Tenant, string, error) {
	limit := clampLimit(f.Limit)

	q := s.db.WithContext(ctx).Model(&tenantModel{})

	if f.Cursor != "" {
		createdAt, id, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("list tenants: %w", err)
		}
		// Perbandingan tuple (created_at, id) > (?, ?) adalah yang membuat
		// seek ini bisa memakai index idx_tenants_created, sekaligus
		// menangani tenant yang created_at-nya sama persis.
		q = q.Where("(created_at, id) > (?, ?)", createdAt, id)
	}

	var models []tenantModel
	if err := q.Order("created_at, id").Limit(limit + 1).Find(&models).Error; err != nil {
		return nil, "", fmt.Errorf("list tenants: %w", translateErr(err))
	}

	next := ""
	if len(models) > limit {
		models = models[:limit]
		last := models[len(models)-1]
		next = encodeCursor(last.CreatedAt, last.ID)
	}

	items := make([]domain.Tenant, 0, len(models))
	for _, m := range models {
		items = append(items, m.toDomain())
	}
	return items, next, nil
}

// Update menyimpan perubahan name dan status.
//
// Hanya dua kolom itu yang ditulis. Slug tidak pernah ikut berubah karena
// ia adalah identitas di URL, dan created_at tidak boleh tertimpa. Memakai
// Select eksplisit lebih aman daripada Save(), yang akan menulis seluruh
// kolom termasuk yang kebetulan kosong di struct.
func (s *TenantStore) Update(ctx context.Context, t *domain.Tenant) error {
	t.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)

	res := s.db.WithContext(ctx).
		Model(&tenantModel{}).
		Where("slug = ?", t.Slug).
		Updates(map[string]any{
			"name":       t.Name,
			"status":     string(t.Status),
			"updated_at": t.UpdatedAt,
		})
	if res.Error != nil {
		return fmt.Errorf("update tenant %q: %w", t.Slug, translateErr(res.Error))
	}
	// UPDATE yang tidak mengenai baris mana pun bukan error bagi SQL, tetapi
	// bagi API ini artinya 404.
	if res.RowsAffected == 0 {
		return fmt.Errorf("update tenant %q: %w", t.Slug, domain.ErrNotFound)
	}
	return nil
}

// Delete menghapus tenant. Agent dan kapabilitas miliknya ikut terhapus
// lewat ON DELETE CASCADE yang didefinisikan di migrasi 0001.
func (s *TenantStore) Delete(ctx context.Context, slug string) error {
	res := s.db.WithContext(ctx).Where("slug = ?", slug).Delete(&tenantModel{})
	if res.Error != nil {
		return fmt.Errorf("delete tenant %q: %w", slug, translateErr(res.Error))
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delete tenant %q: %w", slug, domain.ErrNotFound)
	}
	return nil
}
