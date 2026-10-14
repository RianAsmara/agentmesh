package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// TenantStatus menyatakan apakah sebuah tenant masih boleh dipakai.
type TenantStatus string

// Nilai TenantStatus yang sah.
const (
	// TenantActive berarti tenant beroperasi normal.
	TenantActive TenantStatus = "active"
	// TenantSuspended berarti tenant dinonaktifkan sementara. Datanya tetap
	// ada, tetapi Phase 5 nanti akan menolak traffic-nya.
	TenantSuspended TenantStatus = "suspended"
)

// Valid melaporkan apakah s adalah TenantStatus yang dikenal.
func (s TenantStatus) Valid() bool {
	return s == TenantActive || s == TenantSuspended
}

// Tenant adalah unit isolasi tertinggi di AgentMesh. Semua agent dimiliki
// tepat satu tenant, dan setiap query selalu dibatasi oleh tenant.
type Tenant struct {
	ID uuid.UUID
	// Slug adalah identifier URL-safe yang muncul di path, misalnya "acme"
	// pada /v1/tenants/acme. Sengaja dipisah dari Name karena identifier di
	// URL harus stabil, sedangkan nama tampilan boleh berubah kapan saja.
	Slug string
	// Name adalah nama tampilan, misalnya "Acme Corp".
	Name string
	// Status mengatur apakah tenant aktif atau ditangguhkan.
	Status    TenantStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Validate memeriksa seluruh aturan domain untuk Tenant. Pengecekan
// dilakukan berurutan dan langsung berhenti di pelanggaran pertama,
// sehingga pemanggil selalu mendapat satu field yang paling jelas salah.
func (t Tenant) Validate() error {
	if err := validateSlug("slug", t.Slug, 3, 63); err != nil {
		return err
	}
	if strings.TrimSpace(t.Name) == "" {
		return invalidField("name", "must not be empty or blank")
	}
	if len(t.Name) > 200 {
		return invalidField("name", "must be at most 200 characters")
	}
	if !t.Status.Valid() {
		return invalidField("status", "must be one of: active, suspended")
	}
	return nil
}
