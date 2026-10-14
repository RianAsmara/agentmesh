package domain

import (
	"time"

	"github.com/google/uuid"
)

// Capability adalah satu kemampuan yang diumumkan sebuah agent, misalnya
// "security-review" versi "v1". Inilah yang dicocokkan router di Phase 3
// ketika memilih agent tujuan.
//
// Kapabilitas disimpan sebagai tabel tersendiri, bukan kolom JSONB, agar
// pencarian router menjadi join ber-index biasa dan agar Phase 6 bisa
// mendeteksi drift lewat perbandingan baris, bukan perbandingan JSON.
type Capability struct {
	ID      uuid.UUID
	AgentID uuid.UUID
	// Name adalah nama kemampuan yang dicocokkan router, misalnya
	// "security-review".
	Name string
	// Version memakai bentuk sederhana v1, v2, v3. Sengaja bukan semver:
	// kapabilitas berubah lewat versi mayor saja, sehingga pencocokan di
	// router cukup perbandingan string persis, tanpa resolusi rentang versi.
	Version   string
	CreatedAt time.Time
}

// Validate memeriksa aturan domain untuk Capability.
func (c Capability) Validate() error {
	if err := validateSlug("name", c.Name, 1, 63); err != nil {
		return err
	}
	if !versionPattern.MatchString(c.Version) {
		return invalidField("version", "must match v<number>, for example v1")
	}
	return nil
}
