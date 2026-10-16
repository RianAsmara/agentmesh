package postgres

import (
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
)

// Model GORM di bawah ini sengaja dipisah dari struct domain, meski isinya
// mirip. Kalau keduanya digabung, nama kolom database langsung menjadi nama
// field JSON yang dilihat publik, sehingga satu perubahan skema otomatis
// menjadi perubahan API yang memutus klien. Biaya pemisahan ini adalah
// fungsi pemetaan di bawah; imbalannya adalah skema dan API bisa berubah
// sendiri-sendiri.

// tenantModel memetakan tabel tenants.
type tenantModel struct {
	ID        uuid.UUID `gorm:"column:id;primaryKey"`
	Slug      string    `gorm:"column:slug"`
	Name      string    `gorm:"column:name"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

// TableName memberi tahu GORM nama tabel sebenarnya, tanpa bergantung pada
// aturan pluralisasi otomatisnya.
func (tenantModel) TableName() string { return "tenants" }

// agentModel memetakan tabel agents.
type agentModel struct {
	ID        uuid.UUID `gorm:"column:id;primaryKey"`
	TenantID  uuid.UUID `gorm:"column:tenant_id"`
	Name      string    `gorm:"column:name"`
	Endpoint  string    `gorm:"column:endpoint"`
	Status    string    `gorm:"column:status"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`

	// Capabilities dimuat lewat relasi GORM. foreignKey menunjuk field di
	// capabilityModel, references menunjuk field di struct ini.
	Capabilities []capabilityModel `gorm:"foreignKey:AgentID;references:ID"`
}

func (agentModel) TableName() string { return "agents" }

// capabilityModel memetakan tabel capabilities.
type capabilityModel struct {
	ID        uuid.UUID `gorm:"column:id;primaryKey"`
	AgentID   uuid.UUID `gorm:"column:agent_id"`
	Name      string    `gorm:"column:name"`
	Version   string    `gorm:"column:version"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (capabilityModel) TableName() string { return "capabilities" }

// --- pemetaan domain ke model ---

func tenantFromDomain(t *domain.Tenant) tenantModel {
	return tenantModel{
		ID:        t.ID,
		Slug:      t.Slug,
		Name:      t.Name,
		Status:    string(t.Status),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

func (m tenantModel) toDomain() domain.Tenant {
	return domain.Tenant{
		ID:        m.ID,
		Slug:      m.Slug,
		Name:      m.Name,
		Status:    domain.TenantStatus(m.Status),
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func agentFromDomain(a *domain.Agent) agentModel {
	return agentModel{
		ID:        a.ID,
		TenantID:  a.TenantID,
		Name:      a.Name,
		Endpoint:  a.Endpoint,
		Status:    string(a.Status),
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

func (m agentModel) toDomain() domain.Agent {
	caps := make([]domain.Capability, 0, len(m.Capabilities))
	for _, c := range m.Capabilities {
		caps = append(caps, c.toDomain())
	}
	return domain.Agent{
		ID:           m.ID,
		TenantID:     m.TenantID,
		Name:         m.Name,
		Endpoint:     m.Endpoint,
		Status:       domain.AgentStatus(m.Status),
		Capabilities: caps,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}

func capabilityFromDomain(c *domain.Capability, agentID uuid.UUID) capabilityModel {
	return capabilityModel{
		ID:        c.ID,
		AgentID:   agentID,
		Name:      c.Name,
		Version:   c.Version,
		CreatedAt: c.CreatedAt,
	}
}

func (m capabilityModel) toDomain() domain.Capability {
	return domain.Capability{
		ID:        m.ID,
		AgentID:   m.AgentID,
		Name:      m.Name,
		Version:   m.Version,
		CreatedAt: m.CreatedAt,
	}
}
