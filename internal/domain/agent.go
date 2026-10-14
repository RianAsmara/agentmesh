package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// AgentStatus menyatakan kondisi kesehatan agent yang terakhir diketahui
// control plane.
type AgentStatus string

// Nilai AgentStatus yang sah.
const (
	// AgentUnknown adalah status awal setiap agent yang baru didaftarkan.
	// Phase 1 belum punya sinyal kesehatan apa pun karena agent didaftarkan
	// manual oleh operator; heartbeat baru ada di Phase 2. Memberi nilai
	// awal "healthy" akan membuat router Phase 3 menghubungi agent yang
	// belum pernah terbukti hidup, jadi "unknown" adalah satu-satunya nilai
	// yang jujur di sini.
	AgentUnknown AgentStatus = "unknown"
	// AgentHealthy berarti agent terakhir diketahui siap menerima panggilan.
	AgentHealthy AgentStatus = "healthy"
	// AgentUnhealthy berarti agent terakhir diketahui tidak siap.
	AgentUnhealthy AgentStatus = "unhealthy"
)

// Valid melaporkan apakah s adalah AgentStatus yang dikenal.
func (s AgentStatus) Valid() bool {
	return s == AgentUnknown || s == AgentHealthy || s == AgentUnhealthy
}

// maxEndpointLen membatasi panjang endpoint agar tidak ada kiriman raksasa
// yang masuk ke database. 512 karakter jauh di atas kebutuhan target gRPC
// yang wajar.
const maxEndpointLen = 512

// Agent adalah satu penyedia kemampuan yang terdaftar pada sebuah tenant.
type Agent struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	// Name unik di dalam satu tenant, bukan unik global. Dua tenant berbeda
	// boleh sama-sama punya "security-agent".
	Name string
	// Endpoint adalah target gRPC yang akan dihubungi router di Phase 3,
	// misalnya "dns:///security:9090".
	Endpoint string
	// Status diisi AgentUnknown saat dibuat; lihat komentar pada konstanta.
	Status AgentStatus
	// Capabilities berisi kemampuan yang diumumkan agent ini. Boleh kosong:
	// agent yang terdaftar tanpa kapabilitas sah secara struktur, hanya saja
	// tidak akan pernah terpilih oleh router.
	Capabilities []Capability
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Validate memeriksa aturan domain untuk Agent beserta seluruh
// kapabilitasnya. Error dari kapabilitas dibungkus ulang dengan nama field
// berindeks, misalnya "capabilities[1].name", supaya klien tahu persis
// elemen mana dalam array yang bermasalah.
func (a Agent) Validate() error {
	if err := validateSlug("name", a.Name, 1, 63); err != nil {
		return err
	}
	if strings.TrimSpace(a.Endpoint) == "" {
		return invalidField("endpoint", "must not be empty or blank")
	}
	if len(a.Endpoint) > maxEndpointLen {
		return invalidField("endpoint", "must be at most 512 characters")
	}
	if !a.Status.Valid() {
		return invalidField("status", "must be one of: unknown, healthy, unhealthy")
	}

	// seen mendeteksi kapabilitas kembar di dalam satu permintaan. Tanpa
	// pengecekan ini, duplikat baru ketahuan saat menabrak unique index di
	// tengah transaksi, dan klien hanya menerima 409 tanpa tahu elemen mana
	// yang salah.
	seen := make(map[string]struct{}, len(a.Capabilities))
	for i, c := range a.Capabilities {
		if err := c.Validate(); err != nil {
			var ve ValidationError
			if errors.As(err, &ve) {
				return invalidField("capabilities["+itoa(i)+"]."+ve.Field, ve.Issue)
			}
			return err
		}
		key := c.Name + "@" + c.Version
		if _, dup := seen[key]; dup {
			return invalidField("capabilities["+itoa(i)+"]", "duplicate capability name and version in request")
		}
		seen[key] = struct{}{}
	}
	return nil
}
