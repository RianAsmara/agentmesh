# AgentMesh Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the control plane durable state — PostgreSQL-backed tenants, agents, and capabilities, managed through a versioned REST API.

**Architecture:** Ports and adapters in three layers. `internal/domain` holds entities, validation, and sentinel errors and imports nothing but stdlib + uuid. `internal/store` defines interfaces; `internal/store/postgres` is the only package permitted to import GORM. `internal/api` is the only package permitted to import `net/http`. Dependency arrow runs `api → store → domain` and never backwards, so Phase 5 can add Row-Level Security behind the store interface without touching handlers.

**Tech Stack:** Go 1.26+, GORM v1.31.2 + `gorm.io/driver/postgres` v1.6.3, `github.com/google/uuid` v1.6.0, raw SQL migrations via `go:embed`, testcontainers-go v0.44.0 with its `modules/postgres` helper, PostgreSQL 17, stdlib `net/http` (Go 1.22 method-based routing) and `log/slog`.

**Spec:** `docs/superpowers/specs/2026-10-14-agentmesh-phase1-design.md`

## Global Constraints

- Go 1.26+ (`go.mod` says `go 1.26`; toolchain in use is 1.27.1). Do not bump the directive.
- Module path is `agentmesh`; all internal imports are `agentmesh/internal/...`.
- `internal/domain` may import ONLY the standard library and `github.com/google/uuid`. No GORM, no `net/http`, no `internal/store`, no `internal/api`.
- `internal/store/postgres` is the ONLY package that may import `gorm.io/...` or `github.com/jackc/pgx/v5`.
- `internal/api` is the ONLY package that may import `net/http` (besides `cmd/control-plane`).
- Never use GORM `AutoMigrate`. Schema changes are numbered `.sql` files only.
- All timestamp columns are `TIMESTAMPTZ`. Never bare `TIMESTAMP`.
- UUIDs are generated in Go with `uuid.New()`, never by PostgreSQL defaults.
- Error responses always use the single envelope `{"error":{"code","message","details"}}`. A 500 never echoes a database error string to the client; it logs the detail and returns a generic message.
- Test style follows Phase 0: table-driven subtests, `t.Errorf` for assertions that should continue, `t.Fatalf` for setup failures, no third-party assertion library.
- Lint must pass: `golangci-lint run ./...` with `bodyclose`, `gosec`, `revive` enabled.
- **Komentar dan dokumentasi kode ditulis dalam Bahasa Indonesia.** Doc comment Go tetap diawali nama identifier agar `go doc` dan `revive` tetap benar, lalu penjelasannya Bahasa Indonesia. Contoh: `// Load membaca konfigurasi dari environment variable.` Nama package, fungsi, variabel, pesan error, dan string log tetap Bahasa Inggris.
- Commit after every task. Conventional-commit prefixes (`feat:`, `test:`, `chore:`, `docs:`).

## Review Focus

These are input classes the spec implies but does not give explicit tests for. Each one has a test assigned to the task that owns the code.

1. **Malformed JSON body on POST/PATCH** — a truncated or non-JSON payload must return 400 with the standard envelope, not a panic or a 500. (Tasks 7, 8)
2. **Unknown/garbage query parameters on list endpoints** — `?limit=abc`, `?limit=-5`, `?limit=99999`, `?cursor=notbase64` must return 400 rather than being silently ignored or crashing the pagination SQL. (Task 8)
3. **Tenant slug that is valid-looking but absent** — every nested agent route must 404 on an unknown tenant before touching the agents table, rather than returning an empty list that implies the tenant exists. (Task 8)
4. **Duplicate capabilities inside a single create request** — `[{"name":"x","version":"v1"},{"name":"x","version":"v1"}]` hits the `UNIQUE (agent_id, name, version)` index mid-transaction; the whole create must roll back and return 400, leaving no agent row. (Task 5)
5. **PATCH with an empty or unknown-field body** — must not silently zero out columns. Only `name`/`status` (tenant) and `endpoint`/`status` (agent) are mutable; absent fields stay unchanged. (Tasks 7, 8)

---
## File Structure

| File | Responsibility | Task |
|---|---|---|
| `internal/domain/errors.go` | Sentinel errors + `ValidationError` | 1 |
| `internal/domain/tenant.go` | `Tenant`, `TenantStatus`, validation | 1 |
| `internal/domain/agent.go` | `Agent`, `AgentStatus`, validation | 1 |
| `internal/domain/capability.go` | `Capability`, validation | 1 |
| `internal/domain/validate.go` | Shared slug/name regex helpers | 1 |
| `internal/migrate/migrate.go` | Migration runner + `schema_migrations` | 2 |
| `internal/migrate/migrations/0001_init.sql` | Phase 1 schema | 2 |
| `internal/store/store.go` | `TenantStore`, `AgentStore`, filters, `Page` | 3 |
| `internal/store/postgres/db.go` | GORM connection + pool config | 3 |
| `internal/store/postgres/models.go` | GORM models ↔ domain mapping | 3 |
| `internal/store/postgres/errors.go` | pg/GORM error → domain sentinel | 3 |
| `internal/store/postgres/testutil_test.go` | Shared testcontainer harness | 3 |
| `internal/store/postgres/tenant.go` | `TenantStore` implementation | 4 |
| `internal/store/postgres/agent.go` | `AgentStore` implementation | 5 |
| `internal/store/postgres/cursor.go` | Cursor encode/decode | 5 |
| `internal/api/middleware.go` | recovery, requestID, logging | 6 |
| `internal/api/errors.go` | domain error → HTTP envelope | 7 |
| `internal/api/dto.go` | Request/response types + mapping | 7 |
| `internal/api/tenant.go` | Tenant handlers | 7 |
| `internal/api/agent.go` | Agent handlers | 8 |
| `internal/api/router.go` | Mux + middleware chain + `/readyz` | 8 |
| `internal/config/config.go` | + `DatabaseURL`, pool settings | 9 |
| `cmd/control-plane/main.go` | Wiring | 9 |
| `cmd/seed/main.go` | Demo data loader | 9 |
| `docker-compose.yml` | Local PostgreSQL 17 | 9 |

---

### Task 1: Domain model

**Files:**
- Create: `internal/domain/errors.go`, `internal/domain/validate.go`, `internal/domain/tenant.go`, `internal/domain/agent.go`, `internal/domain/capability.go`
- Test: `internal/domain/tenant_test.go`, `internal/domain/agent_test.go`, `internal/domain/capability_test.go`

**Interfaces:**
- Consumes: nothing (first task).
- Produces:
  - `domain.ErrNotFound`, `domain.ErrAlreadyExists`, `domain.ErrInvalid` (sentinel `error` values)
  - `domain.ValidationError{Field, Issue string}` with `Error() string` and `Unwrap() error` returning `ErrInvalid`
  - `domain.Tenant{ID uuid.UUID; Slug, Name string; Status TenantStatus; CreatedAt, UpdatedAt time.Time}`
  - `domain.TenantStatus` string type, constants `TenantActive = "active"`, `TenantSuspended = "suspended"`
  - `domain.Agent{ID, TenantID uuid.UUID; Name, Endpoint string; Status AgentStatus; Capabilities []Capability; CreatedAt, UpdatedAt time.Time}`
  - `domain.AgentStatus` string type, constants `AgentUnknown = "unknown"`, `AgentHealthy = "healthy"`, `AgentUnhealthy = "unhealthy"`
  - `domain.Capability{ID, AgentID uuid.UUID; Name, Version string; CreatedAt time.Time}`
  - `func (t Tenant) Validate() error`, `func (a Agent) Validate() error`, `func (c Capability) Validate() error`
  - `func (s TenantStatus) Valid() bool`, `func (s AgentStatus) Valid() bool`

- [ ] **Step 1: Add the uuid dependency**

Run: `go get github.com/google/uuid@v1.6.0`
Expected: `go.mod` gains `github.com/google/uuid v1.6.0`.

- [ ] **Step 2: Write the failing tests**

Create `internal/domain/tenant_test.go`:

```go
package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestTenant_Validate(t *testing.T) {
	valid := Tenant{Slug: "acme", Name: "Acme Corp", Status: TenantActive}

	tests := []struct {
		name      string
		mutate    func(*Tenant)
		wantField string // "" berarti valid
	}{
		{"valid tenant", func(*Tenant) {}, ""},
		{"slug minimum length", func(t *Tenant) { t.Slug = "abc" }, ""},
		{"slug with internal dash", func(t *Tenant) { t.Slug = "acme-corp" }, ""},
		{"slug with digits", func(t *Tenant) { t.Slug = "acme2" }, ""},
		{"slug too short", func(t *Tenant) { t.Slug = "ab" }, "slug"},
		{"slug too long", func(t *Tenant) { t.Slug = strings.Repeat("a", 64) }, "slug"},
		{"slug empty", func(t *Tenant) { t.Slug = "" }, "slug"},
		{"slug uppercase", func(t *Tenant) { t.Slug = "Acme" }, "slug"},
		{"slug leading dash", func(t *Tenant) { t.Slug = "-acme" }, "slug"},
		{"slug trailing dash", func(t *Tenant) { t.Slug = "acme-" }, "slug"},
		{"slug underscore", func(t *Tenant) { t.Slug = "acme_corp" }, "slug"},
		{"slug with space", func(t *Tenant) { t.Slug = "acme corp" }, "slug"},
		{"name empty", func(t *Tenant) { t.Name = "" }, "name"},
		{"name blank", func(t *Tenant) { t.Name = "   " }, "name"},
		{"name too long", func(t *Tenant) { t.Name = strings.Repeat("a", 201) }, "name"},
		{"status unknown", func(t *Tenant) { t.Status = "deleted" }, "status"},
		{"status empty", func(t *Tenant) { t.Status = "" }, "status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenant := valid
			tt.mutate(&tenant)

			err := tenant.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error on field %q", tt.wantField)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error is not a ValidationError: %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}

func TestTenantStatus_Valid(t *testing.T) {
	tests := []struct {
		status TenantStatus
		want   bool
	}{
		{TenantActive, true},
		{TenantSuspended, true},
		{"", false},
		{"active ", false},
		{"ACTIVE", false},
		{"deleted", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("TenantStatus(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/domain/...`
Expected: FAIL — build error, `undefined: Tenant`, `undefined: ErrInvalid`, etc.

- [ ] **Step 4: Commit the failing tests**

```bash
git add internal/domain/tenant_test.go go.mod go.sum
git commit -m "test: add failing tenant domain validation tests"
```

- [ ] **Step 5: Write the sentinel errors**

Create `internal/domain/errors.go`:

```go
// Package domain berisi entitas inti AgentMesh beserta aturan validasinya.
//
// Package ini sengaja tidak mengimpor apa pun selain standard library dan
// google/uuid. Tidak ada GORM, tidak ada net/http. Dengan begitu aturan
// bisnis bisa diuji tanpa database dan tanpa HTTP server.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel error yang dipakai lintas lapisan. Lapisan store menerjemahkan
// error database menjadi salah satu dari error ini, lalu lapisan api
// memetakannya ke status code HTTP. Jadi store tidak perlu tahu soal HTTP,
// dan api tidak perlu tahu soal PostgreSQL.
var (
	// ErrNotFound berarti resource yang diminta tidak ada.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists berarti resource dengan identitas yang sama sudah ada.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid berarti input tidak memenuhi aturan validasi domain.
	ErrInvalid = errors.New("invalid")
)

// ValidationError menjelaskan field mana yang gagal validasi dan kenapa.
// Error ini membungkus ErrInvalid sehingga pemanggil bisa memakai
// errors.Is(err, ErrInvalid) untuk pengecekan kasar, atau errors.As untuk
// mengambil detail per field saat menyusun response body.
type ValidationError struct {
	Field string
	Issue string
}

// Error memenuhi interface error.
func (e ValidationError) Error() string {
	return fmt.Sprintf("field %q is invalid: %s", e.Field, e.Issue)
}

// Unwrap membuat errors.Is(err, ErrInvalid) bernilai true.
func (e ValidationError) Unwrap() error { return ErrInvalid }

// invalidField adalah helper ringkas untuk membuat ValidationError.
func invalidField(field, issue string) error {
	return ValidationError{Field: field, Issue: issue}
}
```

- [ ] **Step 6: Write the shared validation helpers**

Create `internal/domain/validate.go`:

```go
package domain

import "regexp"

// slugPattern membatasi identifier yang muncul di URL: huruf kecil, angka,
// dan tanda hubung di tengah. Tidak boleh diawali atau diakhiri tanda
// hubung, tidak boleh underscore, tidak boleh spasi. Aturan ini sama
// dengan yang dipakai DNS label dan Kubernetes resource name, sehingga
// nama yang valid di sini aman dipakai sebagai hostname nanti.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// versionPattern membatasi versi kapabilitas ke bentuk v1, v2, v10.
var versionPattern = regexp.MustCompile(`^v[0-9]+$`)

// validateSlug memeriksa panjang dan bentuk sebuah identifier URL-safe.
// Parameter field dipakai agar pesan error menyebut nama field yang tepat
// ("slug" untuk tenant, "name" untuk agent) meski aturannya sama.
func validateSlug(field, value string, minLen, maxLen int) error {
	if len(value) < minLen || len(value) > maxLen {
		return invalidField(field, "must be between "+itoa(minLen)+" and "+itoa(maxLen)+" characters")
	}
	if !slugPattern.MatchString(value) {
		return invalidField(field, "must contain only lowercase letters, digits, and internal hyphens")
	}
	return nil
}

// itoa membungkus strconv.Itoa agar import di file ini tetap minimal.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
```

- [ ] **Step 7: Write the Tenant entity**

Create `internal/domain/tenant.go`:

```go
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
```

- [ ] **Step 8: Run the tenant tests to verify they pass**

Run: `go test ./internal/domain/... -run 'Tenant' -v`
Expected: PASS for `TestTenant_Validate` (all 17 subtests) and `TestTenantStatus_Valid` (6 subtests).

- [ ] **Step 9: Commit**

```bash
git add internal/domain/errors.go internal/domain/validate.go internal/domain/tenant.go
git commit -m "feat: add tenant domain entity with validation"
```

- [ ] **Step 10: Write the failing agent and capability tests**

Create `internal/domain/agent_test.go`:

```go
package domain

import (
	"errors"
	"strings"
	"testing"
)

func validAgent() Agent {
	return Agent{
		Name:     "security-agent",
		Endpoint: "dns:///security:9090",
		Status:   AgentUnknown,
		Capabilities: []Capability{
			{Name: "security-review", Version: "v1"},
		},
	}
}

func TestAgent_Validate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*Agent)
		wantField string // "" berarti valid
	}{
		{"valid agent", func(*Agent) {}, ""},
		{"single character name", func(a *Agent) { a.Name = "x" }, ""},
		{"no capabilities is allowed", func(a *Agent) { a.Capabilities = nil }, ""},
		{"status healthy", func(a *Agent) { a.Status = AgentHealthy }, ""},
		{"status unhealthy", func(a *Agent) { a.Status = AgentUnhealthy }, ""},
		{"name empty", func(a *Agent) { a.Name = "" }, "name"},
		{"name too long", func(a *Agent) { a.Name = strings.Repeat("a", 64) }, "name"},
		{"name uppercase", func(a *Agent) { a.Name = "Security" }, "name"},
		{"name underscore", func(a *Agent) { a.Name = "security_agent" }, "name"},
		{"endpoint empty", func(a *Agent) { a.Endpoint = "" }, "endpoint"},
		{"endpoint blank", func(a *Agent) { a.Endpoint = "   " }, "endpoint"},
		{"endpoint too long", func(a *Agent) { a.Endpoint = strings.Repeat("a", 513) }, "endpoint"},
		{"status unknown value", func(a *Agent) { a.Status = "degraded" }, "status"},
		{"status empty", func(a *Agent) { a.Status = "" }, "status"},
		{"invalid nested capability", func(a *Agent) {
			a.Capabilities = []Capability{{Name: "Security-Review", Version: "v1"}}
		}, "capabilities[0].name"},
		{"invalid nested capability version", func(a *Agent) {
			a.Capabilities = []Capability{{Name: "security-review", Version: "1.0"}}
		}, "capabilities[0].version"},
		{"second capability invalid reports index 1", func(a *Agent) {
			a.Capabilities = []Capability{
				{Name: "security-review", Version: "v1"},
				{Name: "bad name", Version: "v1"},
			}
		}, "capabilities[1].name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := validAgent()
			tt.mutate(&agent)

			err := agent.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error on field %q", tt.wantField)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error is not a ValidationError: %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}

// TestAgent_ValidateRejectsDuplicateCapabilities menutup Review Focus #4:
// permintaan create yang memuat kapabilitas kembar harus ditolak di lapisan
// domain, jauh sebelum menabrak unique index di database.
func TestAgent_ValidateRejectsDuplicateCapabilities(t *testing.T) {
	agent := validAgent()
	agent.Capabilities = []Capability{
		{Name: "security-review", Version: "v1"},
		{Name: "security-review", Version: "v1"},
	}

	err := agent.Validate()

	if err == nil {
		t.Fatal("Validate() = nil, want duplicate capability error")
	}
	if !errors.Is(err, ErrInvalid) {
		t.Errorf("error does not wrap ErrInvalid: %v", err)
	}
	var ve ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error is not a ValidationError: %v", err)
	}
	if ve.Field != "capabilities[1]" {
		t.Errorf("ValidationError.Field = %q, want %q", ve.Field, "capabilities[1]")
	}
}

// Nama dan versi yang berbeda bukan duplikat; kombinasi keduanya yang unik.
func TestAgent_ValidateAllowsSameNameDifferentVersion(t *testing.T) {
	agent := validAgent()
	agent.Capabilities = []Capability{
		{Name: "security-review", Version: "v1"},
		{Name: "security-review", Version: "v2"},
	}

	if err := agent.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestAgentStatus_Valid(t *testing.T) {
	tests := []struct {
		status AgentStatus
		want   bool
	}{
		{AgentUnknown, true},
		{AgentHealthy, true},
		{AgentUnhealthy, true},
		{"", false},
		{"HEALTHY", false},
		{"degraded", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.Valid(); got != tt.want {
				t.Errorf("AgentStatus(%q).Valid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}
```

Create `internal/domain/capability_test.go`:

```go
package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestCapability_Validate(t *testing.T) {
	valid := Capability{Name: "security-review", Version: "v1"}

	tests := []struct {
		name      string
		mutate    func(*Capability)
		wantField string // "" berarti valid
	}{
		{"valid capability", func(*Capability) {}, ""},
		{"multi digit version", func(c *Capability) { c.Version = "v10" }, ""},
		{"name empty", func(c *Capability) { c.Name = "" }, "name"},
		{"name too long", func(c *Capability) { c.Name = strings.Repeat("a", 64) }, "name"},
		{"name uppercase", func(c *Capability) { c.Name = "Security" }, "name"},
		{"version empty", func(c *Capability) { c.Version = "" }, "version"},
		{"version without v prefix", func(c *Capability) { c.Version = "1" }, "version"},
		{"version semver", func(c *Capability) { c.Version = "v1.0.0" }, "version"},
		{"version uppercase V", func(c *Capability) { c.Version = "V1" }, "version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capability := valid
			tt.mutate(&capability)

			err := capability.Validate()

			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want error on field %q", tt.wantField)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Validate() error does not wrap ErrInvalid: %v", err)
			}
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error is not a ValidationError: %v", err)
			}
			if ve.Field != tt.wantField {
				t.Errorf("ValidationError.Field = %q, want %q", ve.Field, tt.wantField)
			}
		})
	}
}
```

- [ ] **Step 11: Run the tests to verify they fail**

Run: `go test ./internal/domain/...`
Expected: FAIL — `undefined: Agent`, `undefined: Capability`, `undefined: AgentUnknown`.

- [ ] **Step 12: Write the Capability entity**

Create `internal/domain/capability.go`:

```go
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
```

- [ ] **Step 13: Write the Agent entity**

Create `internal/domain/agent.go`:

```go
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
```

- [ ] **Step 14: Run all domain tests to verify they pass**

Run: `go test ./internal/domain/... -v`
Expected: PASS. All of `TestTenant_Validate`, `TestTenantStatus_Valid`, `TestAgent_Validate`, `TestAgent_ValidateRejectsDuplicateCapabilities`, `TestAgent_ValidateAllowsSameNameDifferentVersion`, `TestAgentStatus_Valid`, `TestCapability_Validate`.

- [ ] **Step 15: Verify the domain package imports nothing forbidden**

Run: `go list -deps ./internal/domain | grep -vE '^(internal/|[a-z]+$|[a-z]+/[a-z]+)' | grep -v '^github.com/google/uuid$' | grep -v '^agentmesh'`
Expected: no output. Any line printed means a disallowed dependency (GORM, net/http) crept in — remove it before continuing.

- [ ] **Step 16: Run vet, lint, and the full test suite**

Run: `go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all three exit 0.

- [ ] **Step 17: Commit**

```bash
git add internal/domain/
git commit -m "feat: add agent and capability domain entities with validation"
```

---
### Task 2: Migration runner and initial schema

**Files:**
- Create: `internal/migrate/migrate.go`, `internal/migrate/migrations/0001_init.sql`, `internal/migrate/migrate_test.go`, `internal/migrate/testutil_test.go`, `cmd/migrate/main.go`
- Modify: `Makefile` (the `migrate` target), `.golangci.yml` (exclude generated-free, no change needed — verify only)

**Interfaces:**
- Consumes: nothing from Task 1.
- Produces:
  - `migrate.Run(ctx context.Context, db *sql.DB) error` — applies every pending migration in order, idempotent.
  - `migrate.Applied(ctx context.Context, db *sql.DB) ([]string, error)` — returns applied version strings in order, used by tests and `/readyz` diagnostics.

**Why a hand-written runner instead of golang-migrate:** the whole runner is about 80 lines, has no CLI or driver-registry surface to learn, and keeps the dependency list short. More importantly for a learning project, the thing being taught — "a migrations table records what ran, and you apply the difference inside a transaction" — is the entire mechanism, and it is worth seeing rather than importing.

- [ ] **Step 1: Add the pgx driver dependency**

Run: `go get github.com/jackc/pgx/v5@v5.11.0 && go get github.com/jackc/pgx/v5/stdlib@v5.11.0`
Expected: `go.mod` gains `github.com/jackc/pgx/v5 v5.11.0`. The `stdlib` subpackage provides a `database/sql` driver, which is what the migration runner uses — the runner deliberately does not depend on GORM, so migrations can run before any ORM is wired up.

- [ ] **Step 2: Add the testcontainers dependency**

Run: `go get github.com/testcontainers/testcontainers-go@v0.44.0 && go get github.com/testcontainers/testcontainers-go/modules/postgres@v0.44.0`
Expected: both modules appear in `go.mod`. These are test-only dependencies but Go has no separate test-dependency section, so they land in the main `require` block.

- [ ] **Step 3: Write the shared testcontainer harness**

Create `internal/migrate/testutil_test.go`:

```go
package migrate

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx" ke database/sql
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newTestDB menjalankan satu container PostgreSQL sungguhan dan
// mengembalikan koneksi ke database yang masih kosong.
//
// Kenapa PostgreSQL asli, bukan SQLite atau mock: yang diuji di sini justru
// perilaku khas PostgreSQL — ON DELETE CASCADE, unique violation dengan
// SQLSTATE 23505, dan tipe TIMESTAMPTZ. Database tiruan akan meluluskan
// migrasi yang sebenarnya rusak.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("agentmesh_test"),
		tcpostgres.WithUsername("agentmesh"),
		tcpostgres.WithPassword("agentmesh"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}
```

- [ ] **Step 4: Write the failing migration tests**

Create `internal/migrate/migrate_test.go`:

```go
package migrate

import (
	"context"
	"database/sql"
	"testing"
)

func TestRun_AppliesSchemaFromEmpty(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	for _, table := range []string{"tenants", "agents", "capabilities", "schema_migrations"} {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			                WHERE table_schema = 'public' AND table_name = $1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("query information_schema for %q: %v", table, err)
		}
		if !exists {
			t.Errorf("table %q does not exist after Run()", table)
		}
	}
}

// Menjalankan migrasi dua kali harus aman. Ini kasus nyata: setiap pod yang
// restart akan memanggil Run lagi.
func TestRun_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("first Run() = %v, want nil", err)
	}
	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run() = %v, want nil", err)
	}

	applied, err := Applied(ctx, db)
	if err != nil {
		t.Fatalf("Applied() = %v, want nil", err)
	}
	if len(applied) != 1 {
		t.Fatalf("Applied() returned %d versions (%v), want exactly 1", len(applied), applied)
	}
	if applied[0] != "0001_init" {
		t.Errorf("Applied()[0] = %q, want %q", applied[0], "0001_init")
	}
}

// Memastikan constraint yang dijanjikan spec benar-benar ada di schema.
func TestRun_EnforcesSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	mustExec(t, db, `INSERT INTO tenants (id, slug, name, status)
	                 VALUES ('11111111-1111-1111-1111-111111111111', 'acme', 'Acme', 'active')`)
	mustExec(t, db, `INSERT INTO tenants (id, slug, name, status)
	                 VALUES ('22222222-2222-2222-2222-222222222222', 'globex', 'Globex', 'active')`)

	t.Run("agent name is unique within a tenant", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                 VALUES ('aaaaaaaa-0000-0000-0000-000000000001',
		                         '11111111-1111-1111-1111-111111111111',
		                         'security-agent', 'dns:///s:9090', 'unknown')`)

		_, err := db.ExecContext(ctx, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                               VALUES ('aaaaaaaa-0000-0000-0000-000000000002',
		                                       '11111111-1111-1111-1111-111111111111',
		                                       'security-agent', 'dns:///s:9091', 'unknown')`)
		if err == nil {
			t.Fatal("duplicate agent name within tenant was accepted, want unique violation")
		}
	})

	t.Run("the same agent name is allowed in a different tenant", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                 VALUES ('aaaaaaaa-0000-0000-0000-000000000003',
		                         '22222222-2222-2222-2222-222222222222',
		                         'security-agent', 'dns:///s:9090', 'unknown')`)
	})

	t.Run("deleting a tenant cascades to agents and capabilities", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO capabilities (id, agent_id, name, version)
		                 VALUES ('cccccccc-0000-0000-0000-000000000001',
		                         'aaaaaaaa-0000-0000-0000-000000000001',
		                         'security-review', 'v1')`)

		mustExec(t, db, `DELETE FROM tenants WHERE id = '11111111-1111-1111-1111-111111111111'`)

		var agents, caps int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agents
		    WHERE tenant_id = '11111111-1111-1111-1111-111111111111'`).Scan(&agents); err != nil {
			t.Fatalf("count agents: %v", err)
		}
		if agents != 0 {
			t.Errorf("agents remaining after tenant delete = %d, want 0", agents)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM capabilities
		    WHERE agent_id = 'aaaaaaaa-0000-0000-0000-000000000001'`).Scan(&caps); err != nil {
			t.Fatalf("count capabilities: %v", err)
		}
		if caps != 0 {
			t.Errorf("capabilities remaining after tenant delete = %d, want 0", caps)
		}
	})

	t.Run("timestamps are timezone aware", func(t *testing.T) {
		var dataType string
		err := db.QueryRowContext(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name = 'tenants' AND column_name = 'created_at'`).Scan(&dataType)
		if err != nil {
			t.Fatalf("query column type: %v", err)
		}
		if dataType != "timestamp with time zone" {
			t.Errorf("tenants.created_at type = %q, want %q", dataType, "timestamp with time zone")
		}
	})
}

// mustExec menjalankan satu statement SQL dan menggagalkan test bila error.
// Dipakai untuk menyiapkan data, bukan untuk assertion.
func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("exec %.60s...: %v", query, err)
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/migrate/...`
Expected: FAIL — `undefined: Run`, `undefined: Applied`.

- [ ] **Step 6: Write the initial schema migration**

Create `internal/migrate/migrations/0001_init.sql`:

```sql
-- 0001_init.sql — skema awal Phase 1: tenants, agents, capabilities.
--
-- Catatan desain yang sengaja dibuat eksplisit di sini:
--   * UUID dibuat di sisi Go (uuid.New()), bukan oleh DEFAULT di PostgreSQL,
--     supaya ID sudah ada sebelum INSERT dan bisa dipakai untuk logging dan
--     response tanpa round-trip tambahan.
--   * Semua kolom waktu memakai TIMESTAMPTZ. TIMESTAMP polos tidak menyimpan
--     zona waktu dan diam-diam berarti "waktu lokal server", yang berbeda
--     antara container, laptop, dan CI.

CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    slug       TEXT        NOT NULL UNIQUE,
    name       TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id         UUID PRIMARY KEY,
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    endpoint   TEXT        NOT NULL,
    status     TEXT        NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Multi-tenancy ditegakkan oleh database, bukan oleh kode aplikasi.
    -- Dua tenant boleh sama-sama punya "security-agent"; satu tenant tidak
    -- boleh punya dua. Index inilah yang memenangkan perlombaan antara dua
    -- request POST bersamaan — pengecekan "cek dulu, baru insert" di kode
    -- aplikasi tidak pernah bisa menjamin hal itu.
    CONSTRAINT agents_tenant_name_key UNIQUE (tenant_id, name)
);

CREATE TABLE capabilities (
    id         UUID        PRIMARY KEY,
    agent_id   UUID        NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name       TEXT        NOT NULL,
    version    TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT capabilities_agent_name_version_key UNIQUE (agent_id, name, version)
);

-- Jalur panas router di Phase 3: "agent mana di tenant X yang sehat dan
-- menyediakan kapabilitas Y?". Dua index ini yang melayani join tersebut.
CREATE INDEX idx_capabilities_name    ON capabilities (name);
CREATE INDEX idx_agents_tenant_status ON agents (tenant_id, status);

-- Mendukung paginasi cursor yang stabil: urutan (created_at, id) harus
-- bisa di-seek, bukan di-scan.
CREATE INDEX idx_agents_tenant_created ON agents (tenant_id, created_at, id);
CREATE INDEX idx_tenants_created       ON tenants (created_at, id);
```

- [ ] **Step 7: Write the migration runner**

Create `internal/migrate/migrate.go`:

```go
// Package migrate menjalankan migrasi skema PostgreSQL dari berkas .sql
// yang ditanam ke dalam binary.
//
// Sengaja tidak memakai AutoMigrate milik GORM. AutoMigrate tidak bisa
// menyatakan ON DELETE CASCADE secara andal, tidak pernah menghapus atau
// mengganti nama kolom, dan tidak meninggalkan catatan perubahan yang bisa
// direview. Berkas SQL bernomor meninggalkan jejak yang jelas.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// createMigrationsTable disiapkan terpisah dan dijalankan sebelum migrasi
// mana pun, karena tabel pencatat ini sendiri tidak boleh ikut tercatat.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT        PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Run menerapkan seluruh migrasi yang belum pernah dijalankan, berurutan
// menurut nama berkas. Aman dipanggil berkali-kali: migrasi yang sudah
// tercatat akan dilewati, sehingga setiap pod yang restart boleh memanggil
// fungsi ini tanpa syarat.
func Run(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedSet(ctx, db)
	if err != nil {
		return err
	}

	versions, err := availableVersions()
	if err != nil {
		return err
	}

	for _, version := range versions {
		if _, done := applied[version]; done {
			continue
		}
		if err := applyOne(ctx, db, version); err != nil {
			return err
		}
	}
	return nil
}

// Applied mengembalikan daftar versi yang sudah diterapkan, terurut.
func Applied(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return versions, nil
}

// applyOne menjalankan satu berkas migrasi dan mencatatnya, keduanya di
// dalam satu transaksi. Kalau SQL-nya gagal di tengah jalan, catatan versi
// ikut dibatalkan — jadi tidak pernah ada keadaan "tercatat tapi separuh
// jadi", yang merupakan cara paling umum sebuah sistem migrasi rusak.
func applyOne(ctx context.Context, db *sql.DB, version string) error {
	body, err := migrationFS.ReadFile("migrations/" + version + ".sql")
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op setelah Commit berhasil

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}

// appliedSet membaca versi yang sudah diterapkan sebagai set, untuk
// pengecekan cepat di dalam loop.
func appliedSet(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	versions, err := Applied(ctx, db)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		set[v] = struct{}{}
	}
	return set, nil
}

// availableVersions mendaftar migrasi yang tertanam, terurut menurut nama.
// Penomoran berkas (0001_, 0002_, ...) itulah yang menentukan urutan, jadi
// nomor baru harus selalu lebih besar dari yang sudah ada.
func availableVersions() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(e.Name(), ".sql"))
	}
	sort.Strings(versions)
	return versions, nil
}
```

- [ ] **Step 8: Run the migration tests to verify they pass**

Run: `go test ./internal/migrate/... -v`
Expected: PASS for `TestRun_AppliesSchemaFromEmpty`, `TestRun_IsIdempotent`, and all four subtests of `TestRun_EnforcesSchemaConstraints`. First run pulls the `postgres:17-alpine` image and takes a minute or two; later runs take about ten seconds.

- [ ] **Step 9: Verify -short mode skips the container tests**

Run: `go test -short ./internal/migrate/... -v`
Expected: tests report SKIP with "skipping container-backed test in -short mode", and the package passes in under a second.

- [ ] **Step 10: Write the migrate CLI**

Create `cmd/migrate/main.go`:

```go
// Command migrate menerapkan migrasi skema AgentMesh lalu keluar.
//
// Dipisah dari control-plane sebagai binary tersendiri supaya migrasi bisa
// dijalankan sebagai langkah deploy terpisah — di Kubernetes nanti ini
// menjadi initContainer atau Job, bukan sesuatu yang dijalankan setiap
// replica saat start dan berebut mengunci tabel yang sama.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx"

	"agentmesh/internal/migrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("AGENTMESH_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	if err := migrate.Run(ctx, db); err != nil {
		return err
	}

	applied, err := migrate.Applied(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("migrations applied: %d\n", len(applied))
	for _, v := range applied {
		fmt.Println("  -", v)
	}
	return nil
}
```

- [ ] **Step 11: Wire the Makefile migrate target**

In `Makefile`, replace the stub target:

```makefile
.PHONY: migrate
migrate: ## Apply database migrations (implemented in Phase 1)
	@echo "not implemented yet: database migrations arrive in Phase 1"
```

with:

```makefile
.PHONY: migrate
migrate: ## Apply database migrations
	AGENTMESH_DATABASE_URL="$${AGENTMESH_DATABASE_URL:-postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable}" \
		go run ./cmd/migrate
```

- [ ] **Step 12: Run build, vet, test, and lint**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 13: Commit**

```bash
git add internal/migrate/ cmd/migrate/ Makefile go.mod go.sum
git commit -m "feat: add embedded SQL migration runner and Phase 1 schema"
```

---
### Task 3: Store interfaces, GORM setup, and error translation

**Files:**
- Create: `internal/store/store.go`, `internal/store/postgres/db.go`, `internal/store/postgres/models.go`, `internal/store/postgres/errors.go`, `internal/store/postgres/errors_test.go`, `internal/store/postgres/testutil_test.go`

**Interfaces:**
- Consumes: `domain.Tenant`, `domain.Agent`, `domain.Capability`, `domain.ErrNotFound`, `domain.ErrAlreadyExists` (Task 1); `migrate.Run` (Task 2).
- Produces:
  - `store.TenantStore` and `store.AgentStore` interfaces (full signatures below)
  - `store.AgentFilter{Capability, Status string; Limit int; Cursor string}`
  - `store.TenantFilter{Limit int; Cursor string}`
  - `postgres.Open(ctx context.Context, dsn string, opts Options) (*gorm.DB, error)`
  - `postgres.Options{MaxOpenConns, MaxIdleConns int; ConnMaxLifetime time.Duration}`
  - `postgres.translateErr(err error) error` (unexported, used by Tasks 4 and 5)
  - `postgres.tenantModel`, `postgres.agentModel`, `postgres.capabilityModel` with `toDomain`/`fromDomain` mappers
  - test helper `newTestStore(t *testing.T) *gorm.DB` in `testutil_test.go`

- [ ] **Step 1: Add the GORM dependencies**

Run: `go get gorm.io/gorm@v1.31.2 && go get gorm.io/driver/postgres@v1.6.3`
Expected: both appear in `go.mod`.

- [ ] **Step 2: Write the store interfaces**

Create `internal/store/store.go`:

```go
// Package store mendefinisikan kontrak penyimpanan AgentMesh.
//
// Interface di sini sengaja tidak menyebut GORM, SQL, maupun tipe apa pun
// dari driver database. Itulah sekat yang membuat seluruh rancangan ini
// bekerja: handler HTTP hanya memegang interface, sehingga Phase 5 bisa
// menambahkan Row-Level Security lewat pembungkus transaksi, atau mengganti
// implementasi dengan SQL tulisan tangan, tanpa menyentuh satu baris pun di
// lapisan api.
package store

import (
	"context"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
)

// defaultLimit dan maxLimit membatasi ukuran halaman. Batas atas mencegah
// satu permintaan menarik seluruh tabel ke memori.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// TenantFilter mengatur paginasi daftar tenant.
type TenantFilter struct {
	// Limit adalah jumlah maksimum baris yang dikembalikan. Nilai 0 berarti
	// DefaultLimit.
	Limit int
	// Cursor adalah penanda posisi buram dari halaman sebelumnya. Kosong
	// berarti mulai dari awal.
	Cursor string
}

// AgentFilter mengatur penyaringan dan paginasi daftar agent.
type AgentFilter struct {
	// Capability menyaring agent yang menyediakan nama kapabilitas tertentu.
	// Kosong berarti tanpa penyaringan. Inilah query yang akan dipakai
	// router di Phase 3.
	Capability string
	// Status menyaring menurut status kesehatan agent. Kosong berarti semua.
	Status string
	Limit  int
	Cursor string
}

// TenantStore menyimpan dan mengambil tenant.
type TenantStore interface {
	// Create menyimpan tenant baru. Mengembalikan domain.ErrAlreadyExists
	// bila slug sudah dipakai.
	Create(ctx context.Context, t *domain.Tenant) error
	// GetBySlug mengambil satu tenant. Mengembalikan domain.ErrNotFound
	// bila tidak ada.
	GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error)
	// List mengembalikan satu halaman tenant beserta cursor halaman
	// berikutnya. Cursor kosong berarti tidak ada halaman lagi.
	List(ctx context.Context, f TenantFilter) (items []domain.Tenant, nextCursor string, err error)
	// Update menyimpan perubahan pada name dan status. Slug tidak pernah
	// berubah karena ia adalah identitas di URL.
	Update(ctx context.Context, t *domain.Tenant) error
	// Delete menghapus tenant beserta seluruh agent dan kapabilitasnya
	// lewat ON DELETE CASCADE.
	Delete(ctx context.Context, slug string) error
}

// AgentStore menyimpan dan mengambil agent beserta kapabilitasnya.
//
// Setiap method menerima tenantID dan bukan hanya nama agent. Itu disengaja:
// tidak ada satu pun cara di interface ini untuk mengambil agent tanpa
// menyebut tenant-nya, sehingga kebocoran antar-tenant tidak bisa terjadi
// karena seseorang lupa menambahkan klausa WHERE.
type AgentStore interface {
	// Create menyimpan agent beserta seluruh kapabilitasnya dalam satu
	// transaksi. Mengembalikan domain.ErrAlreadyExists bila nama sudah
	// dipakai di tenant yang sama.
	Create(ctx context.Context, a *domain.Agent) error
	// GetByName mengambil satu agent beserta kapabilitasnya.
	GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Agent, error)
	// List mengembalikan satu halaman agent beserta cursor berikutnya.
	List(ctx context.Context, tenantID uuid.UUID, f AgentFilter) (items []domain.Agent, nextCursor string, err error)
	// Update menyimpan perubahan pada endpoint dan status.
	Update(ctx context.Context, a *domain.Agent) error
	// Delete menghapus agent beserta kapabilitasnya.
	Delete(ctx context.Context, tenantID uuid.UUID, name string) error
}
```

- [ ] **Step 3: Write the connection helper**

Create `internal/store/postgres/db.go`:

```go
// Package postgres adalah implementasi store berbasis PostgreSQL memakai
// GORM.
//
// Ini satu-satunya package di repository yang boleh mengimpor gorm.io/...
// atau driver pgx. Pembatasan itu yang menjaga agar keputusan memakai GORM
// tetap bisa dibatalkan: ADR 0002 merencanakan Row-Level Security sebagai
// lapisan isolasi tenant, dan pooling koneksi GORM membuat SET LOCAL
// app.tenant_id merepotkan. Karena GORM terkurung di sini, Phase 5 bisa
// membungkus atau mengganti isi package ini saja.
package postgres

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Options mengatur pool koneksi database.
type Options struct {
	// MaxOpenConns membatasi total koneksi ke PostgreSQL. Nilai ini harus
	// dijaga: PostgreSQL memakai satu proses per koneksi, jadi batas yang
	// kebesaran justru membebani server, bukan mempercepatnya.
	MaxOpenConns int
	// MaxIdleConns menahan sejumlah koneksi tetap terbuka agar permintaan
	// berikutnya tidak membayar biaya handshake.
	MaxIdleConns int
	// ConnMaxLifetime memaksa koneksi lama didaur ulang, sehingga perubahan
	// DNS atau failover database tidak membuat proses memegang koneksi mati
	// selamanya.
	ConnMaxLifetime time.Duration
}

// Open membuka koneksi GORM ke PostgreSQL dan memverifikasinya dengan ping.
func Open(ctx context.Context, dsn string, opts Options) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// Logger GORM dimatikan: logging permintaan sudah ditangani
		// middleware di lapisan api memakai slog, dan dua sumber log yang
		// berbeda format justru menyulitkan korelasi.
		Logger: logger.Default.LogMode(logger.Silent),
		// Penamaan kolom dan tabel ditentukan oleh tag di models.go dan oleh
		// berkas migrasi, bukan oleh konvensi GORM.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
```

Note on `SkipDefaultTransaction: true`: GORM otherwise wraps every single write in its own transaction, which costs a round trip on operations that do not need it. The one place Phase 1 genuinely needs a transaction — creating an agent with its capabilities — opens one explicitly in Task 5, which is clearer than relying on an implicit default.

- [ ] **Step 4: Write the GORM models and domain mapping**

Create `internal/store/postgres/models.go`:

```go
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
```

- [ ] **Step 5: Write the failing error-translation test**

Create `internal/store/postgres/errors_test.go`:

```go
package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
)

func TestTranslateErr(t *testing.T) {
	uniqueViolation := &pgconn.PgError{Code: "23505", Message: "duplicate key value violates unique constraint"}
	fkViolation := &pgconn.PgError{Code: "23503", Message: "insert or update violates foreign key constraint"}
	otherPgError := &pgconn.PgError{Code: "42P01", Message: "relation does not exist"}

	tests := []struct {
		name string
		in   error
		want error // nil berarti error harus diteruskan apa adanya
	}{
		{"nil stays nil", nil, nil},
		{"unique violation becomes ErrAlreadyExists", uniqueViolation, domain.ErrAlreadyExists},
		{"wrapped unique violation is still detected", fmt.Errorf("create: %w", uniqueViolation), domain.ErrAlreadyExists},
		{"foreign key violation becomes ErrNotFound", fkViolation, domain.ErrNotFound},
		{"gorm record not found becomes ErrNotFound", gorm.ErrRecordNotFound, domain.ErrNotFound},
		{"wrapped gorm not found is still detected", fmt.Errorf("get: %w", gorm.ErrRecordNotFound), domain.ErrNotFound},
		{"unrelated pg error passes through", otherPgError, nil},
		{"unrelated error passes through", errors.New("connection refused"), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := translateErr(tt.in)

			if tt.in == nil {
				if got != nil {
					t.Fatalf("translateErr(nil) = %v, want nil", got)
				}
				return
			}
			if tt.want == nil {
				// Error yang tidak dikenal harus diteruskan utuh, bukan
				// disamarkan menjadi sentinel yang salah.
				if errors.Is(got, domain.ErrAlreadyExists) || errors.Is(got, domain.ErrNotFound) {
					t.Fatalf("translateErr(%v) = %v, want the original error unchanged", tt.in, got)
				}
				if !errors.Is(got, tt.in) {
					t.Errorf("translateErr(%v) = %v, want it to wrap the original", tt.in, got)
				}
				return
			}
			if !errors.Is(got, tt.want) {
				t.Errorf("translateErr(%v) = %v, want it to wrap %v", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 6: Run the test to verify it fails**

Run: `go test ./internal/store/postgres/... -run TestTranslateErr`
Expected: FAIL — `undefined: translateErr`.

- [ ] **Step 7: Write the error translator**

Create `internal/store/postgres/errors.go`:

```go
package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
)

// Kode SQLSTATE PostgreSQL yang diterjemahkan ke sentinel domain.
// Daftar lengkapnya ada di dokumentasi PostgreSQL, bagian "Error Codes".
const (
	// pgUniqueViolation terjadi saat INSERT atau UPDATE menabrak UNIQUE index.
	pgUniqueViolation = "23505"
	// pgForeignKeyViolation terjadi saat baris menunjuk induk yang tidak ada,
	// misalnya membuat agent untuk tenant_id yang sudah terhapus.
	pgForeignKeyViolation = "23503"
)

// translateErr mengubah kosakata database menjadi kosakata domain.
//
// Fungsi kecil inilah satu-satunya selaput antara keduanya. Di atasnya,
// handler hanya mengenal domain.ErrAlreadyExists dan domain.ErrNotFound,
// dan tidak pernah tahu bahwa PostgreSQL ada.
//
// Error yang tidak dikenali diteruskan apa adanya, bukan dipaksa menjadi
// salah satu sentinel. Memaksakan terjemahan akan membuat kegagalan nyata —
// koneksi putus, tabel hilang — menyamar sebagai 404 yang tenang, dan bug
// seperti itu sangat mahal untuk dilacak.
func translateErr(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return errors.Join(domain.ErrAlreadyExists, err)
		case pgForeignKeyViolation:
			return errors.Join(domain.ErrNotFound, err)
		}
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Join(domain.ErrNotFound, err)
	}

	return err
}
```

Note on `errors.Join`: it returns an error that satisfies `errors.Is` for *both* operands, so the caller can match the domain sentinel while the original database error stays attached for the log line. Returning the bare sentinel would be simpler but would throw away the constraint name, which is exactly what you want when debugging a 409 at three in the morning.

- [ ] **Step 8: Run the test to verify it passes**

Run: `go test ./internal/store/postgres/... -run TestTranslateErr -v`
Expected: PASS, all 8 subtests.

- [ ] **Step 9: Write the shared store testcontainer harness**

Create `internal/store/postgres/testutil_test.go`:

```go
package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
	"agentmesh/internal/migrate"
)

// Satu container dipakai bersama oleh seluruh test di package ini.
// Menyalakan PostgreSQL memakan waktu beberapa detik, dan mengulanginya
// untuk setiap test membuat suite terlalu lambat untuk dijalankan sering.
// Isolasi antar-test tetap terjaga karena setiap test bekerja di tenant
// miliknya sendiri; lihat newTenant di bawah.
var (
	sharedOnce sync.Once
	sharedDB   *gorm.DB
	sharedErr  error
)

// testDB mengembalikan koneksi GORM ke PostgreSQL yang skemanya sudah
// dimigrasi.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	sharedOnce.Do(func() {
		sharedDB, sharedErr = startSharedPostgres()
	})
	if sharedErr != nil {
		t.Fatalf("start shared postgres: %v", sharedErr)
	}
	return sharedDB
}

// startSharedPostgres menyalakan container, menjalankan migrasi, lalu
// mengembalikan koneksi GORM.
//
// Container sengaja tidak dihentikan lewat t.Cleanup: ia dipakai bersama,
// jadi test mana pun yang selesai duluan tidak boleh mematikannya. Ryuk,
// container pendamping milik testcontainers, yang akan membersihkannya
// ketika proses test berakhir.
func startSharedPostgres() (*gorm.DB, error) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("agentmesh_test"),
		tcpostgres.WithUsername("agentmesh"),
		tcpostgres.WithPassword("agentmesh"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, err
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}

	// Migrasi dijalankan lewat database/sql, bukan GORM, persis seperti di
	// produksi. Dengan begitu test ini juga membuktikan bahwa model GORM
	// cocok dengan skema yang dihasilkan berkas migrasi — bukan dengan
	// skema yang dikarang GORM sendiri.
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlDB.Close() }()

	if err := migrate.Run(ctx, sqlDB); err != nil {
		return nil, err
	}

	return Open(ctx, dsn, Options{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	})
}

// newTenant membuat satu tenant dengan slug acak dan mengembalikannya.
//
// Setiap test memanggil ini agar punya ruang sendiri di database bersama.
// Karena semua query agent dibatasi tenant_id, test yang berjalan beriringan
// tidak akan saling melihat data — sekaligus menjadi latihan atas sifat
// isolasi tenant yang memang ingin dibuktikan.
func newTenant(t *testing.T, db *gorm.DB) domain.Tenant {
	t.Helper()

	tenant := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "t" + uuid.New().String()[:8],
		Name:   "Test Tenant",
		Status: domain.TenantActive,
	}
	if err := tenant.Validate(); err != nil {
		t.Fatalf("test tenant is invalid: %v", err)
	}

	store := NewTenantStore(db)
	if err := store.Create(context.Background(), &tenant); err != nil {
		t.Fatalf("create test tenant: %v", err)
	}
	return tenant
}
```

Note: `newTenant` calls `NewTenantStore`, which Task 4 creates. This file will not compile until Task 4's Step 3 lands — that is expected and is why Step 10 below only builds the non-test code.

- [ ] **Step 10: Verify the non-test code builds**

Run: `go build ./... && go vet ./internal/store/...`
Expected: both exit 0. (`go test ./internal/store/postgres/...` will not compile yet; Task 4 finishes it.)

- [ ] **Step 11: Verify GORM is confined to one package**

Run: `grep -rln 'gorm.io' --include='*.go' . | grep -v '^./internal/store/postgres/'`
Expected: no output. Any file listed is importing GORM outside the one package allowed to, which breaks the seam this whole design depends on.

- [ ] **Step 12: Commit**

```bash
git add internal/store/ go.mod go.sum
git commit -m "feat: add store interfaces, GORM setup, and error translation"
```

---
### Task 4: TenantStore implementation

**Files:**
- Create: `internal/store/postgres/tenant.go`, `internal/store/postgres/cursor.go`, `internal/store/postgres/cursor_test.go`, `internal/store/postgres/tenant_test.go`

**Interfaces:**
- Consumes: `store.TenantStore`, `store.TenantFilter`, `store.DefaultLimit`, `store.MaxLimit` (Task 3); `domain.Tenant`, `domain.ErrNotFound`, `domain.ErrAlreadyExists` (Task 1); `translateErr`, `tenantModel`, `tenantFromDomain`, `tenantModel.toDomain` (Task 3); `testDB`, `newTenant` (Task 3).
- Produces:
  - `postgres.NewTenantStore(db *gorm.DB) *TenantStore`
  - `postgres.TenantStore` implementing every method of `store.TenantStore`
  - `postgres.encodeCursor(createdAt time.Time, id uuid.UUID) string`
  - `postgres.decodeCursor(s string) (time.Time, uuid.UUID, error)` returning `domain.ErrInvalid` on malformed input
  - `postgres.clampLimit(n int) int`

- [ ] **Step 1: Write the failing cursor test**

Create `internal/store/postgres/cursor_test.go`:

```go
package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

func TestCursorRoundTrip(t *testing.T) {
	wantTime := time.Date(2026, 10, 14, 11, 30, 0, 123456000, time.UTC)
	wantID := uuid.MustParse("0192f1a0-0000-7000-8000-000000000001")

	encoded := encodeCursor(wantTime, wantID)
	if encoded == "" {
		t.Fatal("encodeCursor returned an empty string")
	}

	gotTime, gotID, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor(%q) = %v, want nil", encoded, err)
	}
	if !gotTime.Equal(wantTime) {
		t.Errorf("time = %v, want %v", gotTime, wantTime)
	}
	if gotID != wantID {
		t.Errorf("id = %v, want %v", gotID, wantID)
	}
}

// Cursor datang dari klien, jadi ia adalah input yang tidak tepercaya.
// Bentuk rusak apa pun harus menghasilkan ErrInvalid (yang dipetakan ke
// 400), bukan panic dan bukan 500.
func TestDecodeCursor_RejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"not base64", "!!!not base64!!!"},
		{"base64 of non-json", "aGVsbG8gd29ybGQ="},
		{"base64 of wrong json shape", "eyJmb28iOiJiYXIifQ=="},
		{"base64 of bad uuid", "eyJ0IjoiMjAyNi0xMC0xNFQxMTozMDowMFoiLCJpIjoibm90LWEtdXVpZCJ9"},
		{"empty after prefix", "="},
		{"whitespace", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := decodeCursor(tt.input)

			if err == nil {
				t.Fatalf("decodeCursor(%q) = nil error, want ErrInvalid", tt.input)
			}
			if !errors.Is(err, domain.ErrInvalid) {
				t.Errorf("decodeCursor(%q) error = %v, want it to wrap ErrInvalid", tt.input, err)
			}
		})
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero becomes default", 0, store.DefaultLimit},
		{"negative becomes default", -10, store.DefaultLimit},
		{"in range is unchanged", 25, 25},
		{"max is allowed", store.MaxLimit, store.MaxLimit},
		{"above max is clamped", store.MaxLimit + 1, store.MaxLimit},
		{"absurdly large is clamped", 1_000_000, store.MaxLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clampLimit(tt.in); got != tt.want {
				t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/store/postgres/... -run 'Cursor|ClampLimit'`
Expected: FAIL — `undefined: encodeCursor`, `undefined: decodeCursor`, `undefined: clampLimit`.

- [ ] **Step 3: Write the cursor helpers**

Create `internal/store/postgres/cursor.go`:

```go
package postgres

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// cursorPayload adalah isi cursor sebelum dikodekan. Nama field sengaja
// pendek karena ikut terbawa di setiap URL halaman berikutnya.
type cursorPayload struct {
	T time.Time `json:"t"`
	I string    `json:"i"`
}

// encodeCursor mengemas kunci urut terakhir menjadi satu string buram.
//
// Dipakai paginasi berbasis cursor, bukan OFFSET. Dengan OFFSET, database
// harus membaca lalu membuang baris yang dilewati — OFFSET 10000 membaca
// 10.050 baris untuk mengembalikan 50. Lebih buruk lagi, bila ada baris
// baru masuk di tengah penelusuran, seluruh halaman berikutnya bergeser dan
// ada data yang terlewat tanpa jejak. Cursor membawa kunci urut terakhir,
// sehingga query berikutnya cukup melakukan seek ber-index dan hasilnya
// tetap konsisten walau ada penulisan bersamaan.
//
// Isinya sengaja dibuat buram bagi klien agar urutan bisa diubah kelak
// tanpa memutus siapa pun.
func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	payload := cursorPayload{T: createdAt.UTC(), I: id.String()}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Tidak mungkin terjadi: struct di atas selalu bisa di-marshal.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor membongkar cursor yang dikirim klien.
//
// Seluruh kegagalan dikembalikan sebagai domain.ErrInvalid, bukan error
// internal, karena cursor rusak adalah kesalahan permintaan (400) dan bukan
// kesalahan server (500).
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is not valid base64url", domain.ErrInvalid)
	}

	var payload cursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is not valid JSON", domain.ErrInvalid)
	}
	if payload.T.IsZero() || payload.I == "" {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is missing required fields", domain.ErrInvalid)
	}

	id, err := uuid.Parse(payload.I)
	if err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor contains an invalid id", domain.ErrInvalid)
	}
	return payload.T, id, nil
}

// clampLimit memaksa ukuran halaman berada dalam rentang yang masuk akal.
// Nilai nol atau negatif memakai default; nilai di atas batas dipotong,
// bukan ditolak, agar klien yang meminta terlalu banyak tetap dilayani.
func clampLimit(n int) int {
	switch {
	case n <= 0:
		return store.DefaultLimit
	case n > store.MaxLimit:
		return store.MaxLimit
	default:
		return n
	}
}
```

- [ ] **Step 4: Run the cursor tests to verify they pass**

Run: `go test ./internal/store/postgres/... -run 'Cursor|ClampLimit' -v`
Expected: PASS — `TestCursorRoundTrip`, `TestDecodeCursor_RejectsMalformedInput` (6 subtests), `TestClampLimit` (6 subtests).

- [ ] **Step 5: Write the failing TenantStore tests**

Create `internal/store/postgres/tenant_test.go`:

```go
package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

func TestTenantStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	want := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "t" + uuid.New().String()[:8],
		Name:   "Acme Corp",
		Status: domain.TenantActive,
	}

	if err := s.Create(ctx, &want); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	// Create harus mengisi timestamp, karena pemanggil mengembalikannya
	// dalam response HTTP.
	if want.CreatedAt.IsZero() {
		t.Error("Create() left CreatedAt zero")
	}
	if want.UpdatedAt.IsZero() {
		t.Error("Create() left UpdatedAt zero")
	}

	got, err := s.GetBySlug(ctx, want.Slug)
	if err != nil {
		t.Fatalf("GetBySlug() = %v, want nil", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Slug != want.Slug {
		t.Errorf("Slug = %q, want %q", got.Slug, want.Slug)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Status != domain.TenantActive {
		t.Errorf("Status = %q, want %q", got.Status, domain.TenantActive)
	}
}

func TestTenantStore_CreateDuplicateSlug(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	slug := "t" + uuid.New().String()[:8]
	first := domain.Tenant{ID: uuid.New(), Slug: slug, Name: "First", Status: domain.TenantActive}
	if err := s.Create(ctx, &first); err != nil {
		t.Fatalf("first Create() = %v, want nil", err)
	}

	second := domain.Tenant{ID: uuid.New(), Slug: slug, Name: "Second", Status: domain.TenantActive}
	err := s.Create(ctx, &second)

	if err == nil {
		t.Fatal("second Create() = nil, want ErrAlreadyExists")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("second Create() = %v, want it to wrap ErrAlreadyExists", err)
	}
}

func TestTenantStore_GetMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	_, err := s.GetBySlug(ctx, "does-not-exist-"+uuid.New().String()[:8])

	if err == nil {
		t.Fatal("GetBySlug() = nil error, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetBySlug() = %v, want it to wrap ErrNotFound", err)
	}
}

func TestTenantStore_Update(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	tenant := domain.Tenant{
		ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
		Name: "Before", Status: domain.TenantActive,
	}
	if err := s.Create(ctx, &tenant); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	createdAt := tenant.CreatedAt

	// Jeda kecil agar perubahan UpdatedAt benar-benar terukur.
	time.Sleep(10 * time.Millisecond)

	tenant.Name = "After"
	tenant.Status = domain.TenantSuspended
	if err := s.Update(ctx, &tenant); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.GetBySlug(ctx, tenant.Slug)
	if err != nil {
		t.Fatalf("GetBySlug() = %v, want nil", err)
	}
	if got.Name != "After" {
		t.Errorf("Name = %q, want %q", got.Name, "After")
	}
	if got.Status != domain.TenantSuspended {
		t.Errorf("Status = %q, want %q", got.Status, domain.TenantSuspended)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("Update() changed CreatedAt: got %v, want %v", got.CreatedAt, createdAt)
	}
	if !got.UpdatedAt.After(createdAt) {
		t.Errorf("UpdatedAt = %v, want it after CreatedAt %v", got.UpdatedAt, createdAt)
	}
}

func TestTenantStore_UpdateMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	absent := domain.Tenant{
		ID: uuid.New(), Slug: "absent-" + uuid.New().String()[:8],
		Name: "Nobody", Status: domain.TenantActive,
	}

	err := s.Update(ctx, &absent)

	if err == nil {
		t.Fatal("Update() = nil, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update() = %v, want it to wrap ErrNotFound", err)
	}
}

func TestTenantStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	tenant := domain.Tenant{
		ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
		Name: "Doomed", Status: domain.TenantActive,
	}
	if err := s.Create(ctx, &tenant); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenant.Slug); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}

	if _, err := s.GetBySlug(ctx, tenant.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetBySlug() after delete = %v, want ErrNotFound", err)
	}
}

func TestTenantStore_DeleteMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	err := s.Delete(ctx, "absent-"+uuid.New().String()[:8])

	if err == nil {
		t.Fatal("Delete() = nil, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete() = %v, want it to wrap ErrNotFound", err)
	}
}

// Paginasi diuji dengan prefix slug unik agar tidak terganggu tenant milik
// test lain yang berbagi database yang sama.
func TestTenantStore_ListPaginates(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	const total = 5
	created := make([]domain.Tenant, 0, total)
	for i := 0; i < total; i++ {
		tenant := domain.Tenant{
			ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
			Name: "Page Test", Status: domain.TenantActive,
		}
		if err := s.Create(ctx, &tenant); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
		created = append(created, tenant)
		// Timestamp harus berbeda agar urutan (created_at, id) deterministik.
		time.Sleep(2 * time.Millisecond)
	}

	// Kumpulkan seluruh halaman dengan ukuran 2.
	seen := map[uuid.UUID]int{}
	cursor := ""
	pages := 0
	for {
		items, next, err := s.List(ctx, store.TenantFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(items) > 2 {
			t.Fatalf("List() returned %d items, want at most 2", len(items))
		}
		for _, item := range items {
			seen[item.ID]++
		}
		pages++
		if pages > 100 {
			t.Fatal("List() pagination did not terminate after 100 pages")
		}
		if next == "" {
			break
		}
		cursor = next
	}

	for i, tenant := range created {
		switch seen[tenant.ID] {
		case 1: // benar
		case 0:
			t.Errorf("tenant #%d (%s) never appeared in any page", i, tenant.Slug)
		default:
			t.Errorf("tenant #%d (%s) appeared %d times, want exactly 1", i, tenant.Slug, seen[tenant.ID])
		}
	}
}

func TestTenantStore_ListRejectsBadCursor(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	_, _, err := s.List(ctx, store.TenantFilter{Cursor: "!!!not-a-cursor!!!"})

	if err == nil {
		t.Fatal("List() = nil error, want ErrInvalid")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("List() = %v, want it to wrap ErrInvalid", err)
	}
}
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/store/postgres/... -run TestTenantStore`
Expected: FAIL — `undefined: NewTenantStore`.

- [ ] **Step 7: Write the TenantStore implementation**

Create `internal/store/postgres/tenant.go`:

```go
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
// tersimpan — tanpa perlu membaca ulang barisnya.
func (s *TenantStore) Create(ctx context.Context, t *domain.Tenant) error {
	now := time.Now().UTC()
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
	t.UpdatedAt = time.Now().UTC()

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
```

- [ ] **Step 8: Run the TenantStore tests to verify they pass**

Run: `go test ./internal/store/postgres/... -run 'TestTenantStore' -v`
Expected: PASS for all eight tests: `CreateAndGet`, `CreateDuplicateSlug`, `GetMissing`, `Update`, `UpdateMissing`, `Delete`, `DeleteMissing`, `ListPaginates`, `ListRejectsBadCursor`.

- [ ] **Step 9: Run the full suite**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 10: Commit**

```bash
git add internal/store/postgres/
git commit -m "feat: add PostgreSQL TenantStore with cursor pagination"
```

---
### Task 5: AgentStore implementation

**Files:**
- Create: `internal/store/postgres/agent.go`, `internal/store/postgres/agent_test.go`

**Interfaces:**
- Consumes: `store.AgentStore`, `store.AgentFilter` (Task 3); `domain.Agent`, `domain.Capability`, sentinels (Task 1); `translateErr`, `agentFromDomain`, `capabilityFromDomain`, `agentModel.toDomain` (Task 3); `encodeCursor`, `decodeCursor`, `clampLimit` (Task 4); `testDB`, `newTenant` (Task 3).
- Produces:
  - `postgres.NewAgentStore(db *gorm.DB) *AgentStore`
  - `postgres.AgentStore` implementing every method of `store.AgentStore`

- [ ] **Step 1: Write the failing AgentStore tests**

Create `internal/store/postgres/agent_test.go`:

```go
package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// newAgent membangun agent valid milik tenant tertentu, dengan nama acak
// agar aman dipakai di database bersama.
func newAgent(tenantID uuid.UUID, caps ...domain.Capability) domain.Agent {
	return domain.Agent{
		ID:           uuid.New(),
		TenantID:     tenantID,
		Name:         "a" + uuid.New().String()[:8],
		Endpoint:     "dns:///example:9090",
		Status:       domain.AgentUnknown,
		Capabilities: caps,
	}
}

func cap1(name, version string) domain.Capability {
	return domain.Capability{ID: uuid.New(), Name: name, Version: version}
}

func TestAgentStore_CreateAndGetWithCapabilities(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	want := newAgent(tenant.ID,
		cap1("security-review", "v1"),
		cap1("threat-model", "v2"),
	)

	if err := s.Create(ctx, &want); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if want.CreatedAt.IsZero() {
		t.Error("Create() left CreatedAt zero")
	}

	got, err := s.GetByName(ctx, tenant.ID, want.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Endpoint != want.Endpoint {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, want.Endpoint)
	}
	if got.Status != domain.AgentUnknown {
		t.Errorf("Status = %q, want %q", got.Status, domain.AgentUnknown)
	}
	if len(got.Capabilities) != 2 {
		t.Fatalf("len(Capabilities) = %d, want 2", len(got.Capabilities))
	}

	// Kapabilitas harus kembali dalam urutan yang stabil, agar response API
	// tidak berubah-ubah antar permintaan untuk data yang sama.
	byName := map[string]string{}
	for _, c := range got.Capabilities {
		byName[c.Name] = c.Version
		if c.AgentID != want.ID {
			t.Errorf("capability %q has AgentID %v, want %v", c.Name, c.AgentID, want.ID)
		}
	}
	if byName["security-review"] != "v1" {
		t.Errorf("security-review version = %q, want v1", byName["security-review"])
	}
	if byName["threat-model"] != "v2" {
		t.Errorf("threat-model version = %q, want v2", byName["threat-model"])
	}
}

func TestAgentStore_CreateWithoutCapabilities(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	got, err := s.GetByName(ctx, tenant.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if len(got.Capabilities) != 0 {
		t.Errorf("len(Capabilities) = %d, want 0", len(got.Capabilities))
	}
}

func TestAgentStore_CreateDuplicateNameInSameTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	first := newAgent(tenant.ID)
	if err := s.Create(ctx, &first); err != nil {
		t.Fatalf("first Create() = %v, want nil", err)
	}

	second := newAgent(tenant.ID)
	second.Name = first.Name

	err := s.Create(ctx, &second)

	if err == nil {
		t.Fatal("second Create() = nil, want ErrAlreadyExists")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("second Create() = %v, want it to wrap ErrAlreadyExists", err)
	}
}

// Properti multi-tenant paling dasar: nama yang sama sah di tenant berbeda.
func TestAgentStore_SameNameAllowedInDifferentTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	inA := newAgent(tenantA.ID)
	if err := s.Create(ctx, &inA); err != nil {
		t.Fatalf("create in tenant A = %v, want nil", err)
	}

	inB := newAgent(tenantB.ID)
	inB.Name = inA.Name
	if err := s.Create(ctx, &inB); err != nil {
		t.Fatalf("create same name in tenant B = %v, want nil", err)
	}
}

// Review Focus #4 dan pembuktian batas transaksi: kapabilitas kembar
// menabrak unique index di tengah transaksi, dan seluruh create harus
// dibatalkan — tidak boleh ada baris agent yang tertinggal.
func TestAgentStore_CreateRollsBackWhenCapabilityInsertFails(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID,
		cap1("security-review", "v1"),
		cap1("security-review", "v1"), // duplikat: melanggar unique index
	)

	err := s.Create(ctx, &agent)

	if err == nil {
		t.Fatal("Create() = nil, want an error from the duplicate capability")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("Create() = %v, want it to wrap ErrAlreadyExists", err)
	}

	// Inti test ini: barisnya harus benar-benar hilang, bukan sekadar
	// error yang dikembalikan.
	if _, err := s.GetByName(ctx, tenant.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByName() after failed create = %v, want ErrNotFound — the transaction did not roll back", err)
	}
}

// Properti isolasi tenant yang paling penting di seluruh plan ini.
func TestAgentStore_GetIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	_, err := s.GetByName(ctx, tenantB.ID, agent.Name)

	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant B reading tenant A's agent = %v, want ErrNotFound", err)
	}
}

func TestAgentStore_DeleteIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenantB.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("tenant B deleting tenant A's agent = %v, want ErrNotFound", err)
	}

	// Agent milik tenant A harus tetap utuh.
	if _, err := s.GetByName(ctx, tenantA.ID, agent.Name); err != nil {
		t.Errorf("agent disappeared after a cross-tenant delete attempt: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/store/postgres/... -run TestAgentStore`
Expected: FAIL — `undefined: NewAgentStore`.

- [ ] **Step 3: Write the remaining AgentStore tests**

Append to `internal/store/postgres/agent_test.go`:

```go
func TestAgentStore_Update(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	agent.Endpoint = "dns:///moved:9999"
	agent.Status = domain.AgentHealthy
	if err := s.Update(ctx, &agent); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.GetByName(ctx, tenant.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.Endpoint != "dns:///moved:9999" {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, "dns:///moved:9999")
	}
	if got.Status != domain.AgentHealthy {
		t.Errorf("Status = %q, want %q", got.Status, domain.AgentHealthy)
	}
	// Update hanya menyentuh endpoint dan status; kapabilitas tidak ikut
	// terhapus.
	if len(got.Capabilities) != 1 {
		t.Errorf("len(Capabilities) after update = %d, want 1", len(got.Capabilities))
	}
}

func TestAgentStore_UpdateIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	impostor := agent
	impostor.TenantID = tenantB.ID
	impostor.Endpoint = "dns:///hijacked:1234"

	if err := s.Update(ctx, &impostor); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant Update() = %v, want ErrNotFound", err)
	}

	got, err := s.GetByName(ctx, tenantA.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.Endpoint == "dns:///hijacked:1234" {
		t.Error("a cross-tenant Update() modified the agent — tenant scoping is broken")
	}
}

func TestAgentStore_Delete(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenant.ID, agent.Name); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}

	if _, err := s.GetByName(ctx, tenant.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByName() after delete = %v, want ErrNotFound", err)
	}

	// Kapabilitas harus ikut terhapus lewat ON DELETE CASCADE.
	var remaining int64
	if err := db.Table("capabilities").Where("agent_id = ?", agent.ID).Count(&remaining).Error; err != nil {
		t.Fatalf("count capabilities: %v", err)
	}
	if remaining != 0 {
		t.Errorf("capabilities remaining after agent delete = %d, want 0", remaining)
	}
}

// Query yang akan dipakai router di Phase 3.
func TestAgentStore_ListFiltersByCapability(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	reviewer := newAgent(tenant.ID, cap1("security-review", "v1"))
	planner := newAgent(tenant.ID, cap1("plan-work", "v1"))
	both := newAgent(tenant.ID, cap1("security-review", "v1"), cap1("plan-work", "v1"))

	for i, a := range []*domain.Agent{&reviewer, &planner, &both} {
		if err := s.Create(ctx, a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
	}

	items, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Capability: "security-review"})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	found := map[uuid.UUID]bool{}
	for _, a := range items {
		found[a.ID] = true
	}
	if !found[reviewer.ID] {
		t.Error("reviewer agent missing from capability-filtered list")
	}
	if !found[both.ID] {
		t.Error("multi-capability agent missing from capability-filtered list")
	}
	if found[planner.ID] {
		t.Error("planner agent appeared in a list filtered by a capability it does not have")
	}

	// Agent yang punya dua kapabilitas tidak boleh muncul dua kali hanya
	// karena join menghasilkan dua baris.
	seen := map[uuid.UUID]int{}
	for _, a := range items {
		seen[a.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("agent %v appeared %d times in one page, want exactly 1", id, n)
		}
	}
}

func TestAgentStore_ListFiltersByStatus(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	healthy := newAgent(tenant.ID)
	healthy.Status = domain.AgentHealthy
	unknown := newAgent(tenant.ID)

	for i, a := range []*domain.Agent{&healthy, &unknown} {
		if err := s.Create(ctx, a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
	}

	items, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Status: string(domain.AgentHealthy)})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	for _, a := range items {
		if a.Status != domain.AgentHealthy {
			t.Errorf("agent %q has status %q in a healthy-filtered list", a.Name, a.Status)
		}
	}

	found := false
	for _, a := range items {
		if a.ID == healthy.ID {
			found = true
		}
		if a.ID == unknown.ID {
			t.Error("unknown-status agent appeared in a healthy-filtered list")
		}
	}
	if !found {
		t.Error("healthy agent missing from healthy-filtered list")
	}
}

func TestAgentStore_ListIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	inA := newAgent(tenantA.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &inA); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	items, _, err := s.List(ctx, tenantB.ID, store.AgentFilter{})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	for _, a := range items {
		if a.ID == inA.ID {
			t.Fatal("tenant B's list contained tenant A's agent — tenant scoping is broken")
		}
	}
}

func TestAgentStore_ListPaginates(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	const total = 5
	created := make([]uuid.UUID, 0, total)
	for i := 0; i < total; i++ {
		a := newAgent(tenant.ID, cap1("shared-capability", "v1"))
		if err := s.Create(ctx, &a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
		created = append(created, a.ID)
		time.Sleep(2 * time.Millisecond)
	}

	seen := map[uuid.UUID]int{}
	cursor := ""
	pages := 0
	for {
		items, next, err := s.List(ctx, tenant.ID, store.AgentFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(items) > 2 {
			t.Fatalf("List() returned %d items, want at most 2", len(items))
		}
		for _, a := range items {
			seen[a.ID]++
		}
		pages++
		if pages > 100 {
			t.Fatal("List() pagination did not terminate after 100 pages")
		}
		if next == "" {
			break
		}
		cursor = next
	}

	for i, id := range created {
		if seen[id] != 1 {
			t.Errorf("agent #%d appeared %d times across all pages, want exactly 1", i, seen[id])
		}
	}
}

// Review Focus #2 di lapisan store: cursor rusak harus menjadi ErrInvalid.
func TestAgentStore_ListRejectsBadCursor(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	_, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Cursor: "!!!garbage!!!"})

	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("List() with a bad cursor = %v, want ErrInvalid", err)
	}
}

// Dua permintaan create identik yang berjalan bersamaan: tepat satu boleh
// berhasil. Yang memutuskan adalah unique index, bukan kode aplikasi.
func TestAgentStore_ConcurrentCreateYieldsExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	name := "race" + uuid.New().String()[:8]

	const attempts = 2
	var wg sync.WaitGroup
	errs := make([]error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := newAgent(tenant.ID, cap1("security-review", "v1"))
			a.Name = name
			errs[i] = s.Create(ctx, &a)
		}(i)
	}
	wg.Wait()

	var succeeded, conflicted int
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrAlreadyExists):
			conflicted++
		default:
			t.Errorf("attempt %d returned an unexpected error: %v", i, err)
		}
	}
	if succeeded != 1 {
		t.Errorf("successful creates = %d, want exactly 1", succeeded)
	}
	if conflicted != 1 {
		t.Errorf("conflicting creates = %d, want exactly 1", conflicted)
	}
}
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `go test ./internal/store/postgres/... -run TestAgentStore`
Expected: FAIL — still `undefined: NewAgentStore`.

- [ ] **Step 5: Write the AgentStore implementation**

Create `internal/store/postgres/agent.go`:

```go
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
	now := time.Now().UTC()
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
	a.UpdatedAt = time.Now().UTC()

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
```

Note on `Omit("Capabilities")` in `Create`: GORM would otherwise try to save the associated capabilities automatically as part of creating the parent row, using its own upsert semantics. Inserting them explicitly in the loop below keeps the behavior visible and lets each insert's error be translated individually — which is what makes the rollback test meaningful rather than accidental.

- [ ] **Step 6: Run the AgentStore tests to verify they pass**

Run: `go test ./internal/store/postgres/... -run TestAgentStore -v`
Expected: PASS for all fourteen tests, including `CreateRollsBackWhenCapabilityInsertFails`, `GetIsScopedToTenant`, `ListFiltersByCapability`, and `ConcurrentCreateYieldsExactlyOneWinner`.

- [ ] **Step 7: Run the whole store package with the race detector**

Run: `go test -race ./internal/store/...`
Expected: PASS, no race reports. The concurrency test above is the one that matters here.

- [ ] **Step 8: Run build, vet, and lint**

Run: `go build ./... && go vet ./... && golangci-lint run ./...`
Expected: all three exit 0.

- [ ] **Step 9: Commit**

```bash
git add internal/store/postgres/
git commit -m "feat: add PostgreSQL AgentStore with transactional create and capability filtering"
```

---
### Task 6: HTTP middleware

**Files:**
- Create: `internal/api/middleware.go`, `internal/api/middleware_test.go`

**Interfaces:**
- Consumes: `observability.NewLogger` (Phase 0, existing).
- Produces:
  - `api.RequestIDHeader = "X-Request-ID"` constant
  - `api.WithRecovery(logger *slog.Logger) func(http.Handler) http.Handler`
  - `api.WithRequestID() func(http.Handler) http.Handler`
  - `api.WithLogging(logger *slog.Logger) func(http.Handler) http.Handler`
  - `api.RequestIDFrom(ctx context.Context) string`
  - `api.LoggerFrom(ctx context.Context) *slog.Logger` — returns the request-scoped logger, falling back to `slog.Default()`
  - `api.Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler`

- [ ] **Step 1: Write the failing middleware tests**

Create `internal/api/middleware_test.go`:

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentmesh/internal/observability"
)

func TestWithRequestID_GeneratesWhenAbsent(t *testing.T) {
	var seen string
	handler := WithRequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("RequestIDFrom(ctx) = \"\", want a generated id")
	}
	if got := rec.Header().Get(RequestIDHeader); got != seen {
		t.Errorf("response header %s = %q, want %q", RequestIDHeader, got, seen)
	}
}

func TestWithRequestID_PreservesInboundHeader(t *testing.T) {
	const inbound = "trace-from-upstream-123"

	var seen string
	handler := WithRequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(RequestIDHeader, inbound)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if seen != inbound {
		t.Errorf("RequestIDFrom(ctx) = %q, want the inbound id %q", seen, inbound)
	}
	if got := rec.Header().Get(RequestIDHeader); got != inbound {
		t.Errorf("response header = %q, want %q", got, inbound)
	}
}

// Header dari luar adalah input tak tepercaya. Nilai yang terlalu panjang
// atau memuat karakter aneh tidak boleh diteruskan apa adanya ke log,
// karena byte newline di sana bisa memalsukan baris log tambahan.
func TestWithRequestID_RejectsUnsafeInboundValues(t *testing.T) {
	tests := []struct {
		name    string
		inbound string
	}{
		{"newline injection", "abc\ndef"},
		{"carriage return", "abc\rdef"},
		{"too long", strings.Repeat("a", 200)},
		{"non printable", "abc\x00def"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			handler := WithRequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = RequestIDFrom(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(RequestIDHeader, tt.inbound)
			handler.ServeHTTP(httptest.NewRecorder(), req)

			if seen == tt.inbound {
				t.Errorf("unsafe inbound request id %q was used verbatim", tt.inbound)
			}
			if seen == "" {
				t.Error("rejecting the inbound id must still yield a generated one")
			}
		})
	}
}

func TestWithRecovery_ConvertsPanicToFiveHundred(t *testing.T) {
	var logBuf bytes.Buffer
	logger := observability.NewLogger(&logBuf, "test", "error")

	handler := WithRecovery(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(logBuf.String(), "panic") {
		t.Errorf("log output does not mention the panic: %s", logBuf.String())
	}
	// Detail panic tidak boleh bocor ke klien.
	if strings.Contains(rec.Body.String(), "handler exploded") {
		t.Errorf("response body leaked the panic message: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestWithRecovery_PassesThroughNormalResponses(t *testing.T) {
	var logBuf bytes.Buffer
	logger := observability.NewLogger(&logBuf, "test", "error")

	handler := WithRecovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("fine"))
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTeapot)
	}
	if rec.Body.String() != "fine" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "fine")
	}
}

func TestWithLogging_EmitsOneStructuredLine(t *testing.T) {
	var logBuf bytes.Buffer
	logger := observability.NewLogger(&logBuf, "test", "info")

	handler := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
		WithRequestID(),
		WithLogging(logger),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", nil)
	req.Header.Set(RequestIDHeader, "req-abc")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("log produced %d lines, want exactly 1:\n%s", len(lines), logBuf.String())
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("log line is not valid JSON: %v\n%s", err, lines[0])
	}

	for field, want := range map[string]any{
		"method":     "POST",
		"path":       "/v1/tenants",
		"status":     float64(http.StatusCreated), // JSON number
		"request_id": "req-abc",
	} {
		got, ok := entry[field]
		if !ok {
			t.Errorf("log entry is missing field %q: %v", field, entry)
			continue
		}
		if got != want {
			t.Errorf("log field %q = %v, want %v", field, got, want)
		}
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Errorf("log entry is missing duration_ms: %v", entry)
	}
}

func TestLoggerFrom_FallsBackWhenAbsent(t *testing.T) {
	// Context tanpa logger tidak boleh membuat pemanggil nil-panic.
	if LoggerFrom(httptest.NewRequest(http.MethodGet, "/", nil).Context()) == nil {
		t.Fatal("LoggerFrom(ctx) = nil, want a usable fallback logger")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — `undefined: WithRequestID`, `undefined: WithRecovery`, `undefined: Chain`.

- [ ] **Step 3: Write the middleware**

Create `internal/api/middleware.go`:

```go
// Package api berisi lapisan HTTP control plane: handler, DTO, pemetaan
// error, dan middleware.
//
// Ini satu-satunya package internal yang boleh mengimpor net/http. Handler
// di sini hanya memegang interface dari package store, dan tidak pernah
// melihat *gorm.DB — itulah yang membuat keputusan database bisa diubah
// tanpa menyentuh lapisan ini.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
)

// RequestIDHeader adalah nama header yang membawa identitas permintaan,
// baik yang datang dari luar maupun yang dikembalikan dalam response.
const RequestIDHeader = "X-Request-ID"

// maxRequestIDLen membatasi panjang request id yang diterima dari luar.
const maxRequestIDLen = 128

// contextKey adalah tipe privat untuk kunci context. Tipe tersendiri
// mencegah tabrakan dengan package lain yang kebetulan memakai string yang
// sama sebagai kunci.
type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

// RequestIDFrom mengambil request id dari context. Mengembalikan string
// kosong bila tidak ada.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// LoggerFrom mengambil logger yang sudah dilekati request id.
//
// Lewat context, bukan lewat parameter fungsi: inilah jawaban atas
// pertanyaan terbuka yang ditinggalkan panduan belajar Phase 0 — bagaimana
// menambahkan field per-permintaan ke setiap baris log tanpa mengoper
// logger melalui setiap tanda tangan fungsi.
//
// Selalu mengembalikan logger yang bisa dipakai, tidak pernah nil, supaya
// kode pemanggil tidak perlu menjaga-jaga.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// Chain merangkai middleware di sekeliling handler.
//
// Middleware pertama di daftar berada paling luar, sehingga urutan
// penulisan sama dengan urutan eksekusi — Chain(h, A, B) berarti A
// membungkus B membungkus h.
func Chain(h http.Handler, mw ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// WithRequestID memastikan setiap permintaan punya identitas.
//
// Bila klien mengirim X-Request-ID yang wajar, nilainya dipakai kembali
// agar jejak permintaan menyambung melintasi beberapa layanan. Bila tidak,
// atau bila nilainya mencurigakan, dibuatkan yang baru. Hasilnya
// dikembalikan lewat header response supaya klien bisa menyebut id itu
// ketika melaporkan masalah.
func WithRequestID() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := sanitizeRequestID(r.Header.Get(RequestIDHeader))
			if id == "" {
				id = uuid.New().String()
			}

			w.Header().Set(RequestIDHeader, id)
			ctx := context.WithValue(r.Context(), requestIDKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sanitizeRequestID menolak nilai header yang tidak aman.
//
// Yang dijaga di sini adalah log injection: sebuah byte newline di dalam
// request id akan muncul sebagai baris log tambahan yang tampak sah, dan
// penyerang bisa memakainya untuk mengarang entri palsu. Nilai yang
// ditolak dikembalikan sebagai string kosong, lalu pemanggil membuat id
// baru — permintaan tetap dilayani, hanya id dari luar yang dibuang.
func sanitizeRequestID(v string) string {
	if v == "" || len(v) > maxRequestIDLen {
		return ""
	}
	for _, r := range v {
		// Hanya ASCII yang bisa dicetak, tanpa spasi.
		if r < '!' || r > '~' {
			return ""
		}
	}
	return v
}

// statusRecorder membungkus http.ResponseWriter untuk mengingat status
// code yang ditulis handler.
//
// Diperlukan karena http.ResponseWriter tidak menyediakan cara membaca
// kembali status yang sudah ditulis, padahal middleware logging perlu
// mencatatnya.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

// WriteHeader mencatat status lalu meneruskannya.
func (r *statusRecorder) WriteHeader(status int) {
	if !r.wrote {
		r.status = status
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(status)
}

// Write menangani handler yang langsung menulis body tanpa memanggil
// WriteHeader; dalam hal itu net/http menyiratkan status 200.
func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}

// WithLogging mencatat tepat satu baris terstruktur per permintaan.
//
// Satu baris per permintaan, bukan satu saat masuk dan satu saat keluar:
// separuh biaya penyimpanan log, dan setiap baris sudah lengkap dengan
// status serta durasinya, sehingga bisa langsung diagregasi tanpa
// menggabungkan dua entri.
func WithLogging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			requestLogger := logger.With("request_id", RequestIDFrom(r.Context()))
			ctx := context.WithValue(r.Context(), loggerKey, requestLogger)

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r.WithContext(ctx))

			requestLogger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
			)
		})
	}
}

// WithRecovery mengubah panic menjadi response 500.
//
// Tanpa ini, satu panic di satu handler akan menjatuhkan seluruh proses
// dan memutus semua permintaan lain yang sedang berjalan. Pesan panic
// hanya masuk ke log, tidak pernah ke body response — isinya bisa memuat
// potongan state internal.
func WithRecovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}

				logger.Error("panic recovered in HTTP handler",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path,
					"request_id", RequestIDFrom(r.Context()),
				)

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"code":"internal","message":"internal server error"}}`))
			}()

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run the middleware tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS for `TestWithRequestID_GeneratesWhenAbsent`, `TestWithRequestID_PreservesInboundHeader`, `TestWithRequestID_RejectsUnsafeInboundValues` (4 subtests), `TestWithRecovery_ConvertsPanicToFiveHundred`, `TestWithRecovery_PassesThroughNormalResponses`, `TestWithLogging_EmitsOneStructuredLine`, `TestLoggerFrom_FallsBackWhenAbsent`.

- [ ] **Step 5: Run build, vet, race, and lint**

Run: `go build ./... && go vet ./... && go test -race ./internal/api/... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/api/
git commit -m "feat: add HTTP middleware for recovery, request IDs, and logging"
```

---
### Task 7: Error envelope, DTOs, and tenant handlers

**Files:**
- Create: `internal/api/errors.go`, `internal/api/dto.go`, `internal/api/tenant.go`, `internal/api/errors_test.go`, `internal/api/tenant_test.go`

**Interfaces:**
- Consumes: `store.TenantStore`, `store.TenantFilter` (Task 3); `domain.Tenant`, `domain.ValidationError`, sentinels (Task 1); `LoggerFrom` (Task 6).
- Produces:
  - `api.errorResponse`, `api.errorBody`, `api.errorDetail` (unexported response shapes)
  - `api.writeError(w http.ResponseWriter, r *http.Request, err error)`
  - `api.writeJSON(w http.ResponseWriter, status int, v any)`
  - `api.decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error`
  - `api.tenantResponse`, `api.createTenantRequest`, `api.patchTenantRequest`, `api.listResponse[T any]`
  - `api.TenantHandler` with `Create`, `Get`, `List`, `Patch`, `Delete` methods and `NewTenantHandler(store.TenantStore) *TenantHandler`

- [ ] **Step 1: Write the failing error-mapping test**

Create `internal/api/errors_test.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentmesh/internal/domain"
)

func TestWriteError_MapsDomainErrorsToStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"not found", domain.ErrNotFound, http.StatusNotFound, "not_found"},
		{"wrapped not found", fmt.Errorf("get tenant: %w", domain.ErrNotFound), http.StatusNotFound, "not_found"},
		{"already exists", domain.ErrAlreadyExists, http.StatusConflict, "already_exists"},
		{"invalid", domain.ErrInvalid, http.StatusBadRequest, "invalid"},
		{"validation error", domain.ValidationError{Field: "slug", Issue: "too short"}, http.StatusBadRequest, "invalid"},
		{"unknown error", errors.New("connection refused"), http.StatusInternalServerError, "internal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)

			writeError(rec, req, tt.err)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}

			var body errorResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not valid JSON: %v\n%s", err, rec.Body.String())
			}
			if body.Error.Code != tt.wantCode {
				t.Errorf("error.code = %q, want %q", body.Error.Code, tt.wantCode)
			}
			if body.Error.Message == "" {
				t.Error("error.message is empty")
			}
		})
	}
}

// Error 500 tidak boleh membocorkan detail internal ke klien. Detailnya
// tetap dicatat di log, tetapi body response harus generik.
func TestWriteError_DoesNotLeakInternalDetail(t *testing.T) {
	const secret = `pq: duplicate key value violates unique constraint "idx_agents_tenant_name"`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	writeError(rec, req, errors.New(secret))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "idx_agents_tenant_name") || strings.Contains(body, "pq:") {
		t.Errorf("500 response leaked internal detail: %s", body)
	}
}

// ValidationError harus membawa nama field ke dalam details, supaya klien
// bisa menunjuk input mana yang salah.
func TestWriteError_IncludesValidationFieldDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	writeError(rec, req, domain.ValidationError{Field: "slug", Issue: "must be lowercase"})

	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(body.Error.Details) != 1 {
		t.Fatalf("len(details) = %d, want 1: %+v", len(body.Error.Details), body.Error)
	}
	if body.Error.Details[0].Field != "slug" {
		t.Errorf("details[0].field = %q, want %q", body.Error.Details[0].Field, "slug")
	}
	if body.Error.Details[0].Issue != "must be lowercase" {
		t.Errorf("details[0].issue = %q, want %q", body.Error.Details[0].Issue, "must be lowercase")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/... -run TestWriteError`
Expected: FAIL — `undefined: writeError`, `undefined: errorResponse`.

- [ ] **Step 3: Write the error envelope**

Create `internal/api/errors.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"agentmesh/internal/domain"
)

// Satu bentuk error untuk seluruh API. Karena bentuknya tunggal, CLI dan
// dashboard cukup menulis satu parser kegagalan, bukan satu per endpoint.
type (
	// errorResponse adalah pembungkus terluar body error.
	errorResponse struct {
		Error errorBody `json:"error"`
	}

	// errorBody memuat kode mesin, pesan untuk manusia, dan detail per field.
	errorBody struct {
		Code    string        `json:"code"`
		Message string        `json:"message"`
		Details []errorDetail `json:"details,omitempty"`
	}

	// errorDetail menunjuk satu field yang bermasalah.
	errorDetail struct {
		Field string `json:"field"`
		Issue string `json:"issue"`
	}
)

// Kode error yang dikenal klien. Ini bagian dari kontrak API, jadi nilainya
// tidak boleh berubah sembarangan; pesan boleh berubah, kode tidak.
const (
	codeInvalid       = "invalid"
	codeNotFound      = "not_found"
	codeAlreadyExists = "already_exists"
	codeInternal      = "internal"
)

// writeError memetakan error domain menjadi response HTTP.
//
// Pemetaan terpusat di sini supaya tidak ada handler yang diam-diam
// memilih status code sendiri. Kalau suatu saat ErrNotFound perlu menjadi
// 410 alih-alih 404, satu tempat ini yang diubah.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := classify(err)

	body := errorResponse{Error: errorBody{Code: code, Message: message}}

	// Detail per field hanya ada pada kegagalan validasi.
	var ve domain.ValidationError
	if errors.As(err, &ve) {
		body.Error.Details = []errorDetail{{Field: ve.Field, Issue: ve.Issue}}
	}

	// Error 500 selalu dicatat lengkap, karena hanya log yang menyimpan
	// penyebab sebenarnya — body response sengaja dibuat generik.
	if status == http.StatusInternalServerError {
		LoggerFrom(r.Context()).Error("request failed",
			"error", err.Error(),
			"method", r.Method,
			"path", r.URL.Path,
		)
	}

	writeJSON(w, status, body)
}

// classify menentukan status, kode, dan pesan dari sebuah error.
//
// Urutan pengecekan penting: ValidationError membungkus ErrInvalid, jadi
// ia harus diperiksa sebelum pengecekan ErrInvalid yang lebih umum agar
// pesannya yang lebih informatif tidak tertimpa.
func classify(err error) (status int, code, message string) {
	var ve domain.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, codeInvalid, ve.Error()
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusBadRequest, codeInvalid, "the request is invalid"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, codeNotFound, "the requested resource does not exist"
	case errors.Is(err, domain.ErrAlreadyExists):
		return http.StatusConflict, codeAlreadyExists, "a resource with that identity already exists"
	default:
		// Pesan generik. Mengirim err.Error() apa adanya ke klien akan
		// membocorkan nama constraint, nama tabel, dan potongan DSN —
		// peta skema gratis bagi penyerang.
		return http.StatusInternalServerError, codeInternal, "internal server error"
	}
}

// writeJSON menulis satu nilai sebagai JSON beserta header-nya.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Error di sini berarti koneksi sudah putus di tengah penulisan;
	// status sudah terkirim sehingga tidak ada lagi yang bisa dilakukan.
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run the error tests to verify they pass**

Run: `go test ./internal/api/... -run TestWriteError -v`
Expected: PASS — `MapsDomainErrorsToStatusCodes` (6 subtests), `DoesNotLeakInternalDetail`, `IncludesValidationFieldDetail`.

- [ ] **Step 5: Write the DTOs**

Create `internal/api/dto.go`:

```go
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"agentmesh/internal/domain"
)

// maxBodyBytes membatasi ukuran body permintaan. Tanpa batas ini, satu
// klien bisa membuat server kehabisan memori hanya dengan mengirim JSON
// yang sangat panjang.
const maxBodyBytes = 1 << 20 // 1 MiB

// DTO di bawah ini terpisah dari struct domain dengan sengaja. Nama field
// JSON adalah kontrak publik; nama field domain adalah urusan internal.
// Memisahkannya berarti keduanya bisa berubah sendiri-sendiri.

// tenantResponse adalah bentuk tenant yang dikirim ke klien.
type tenantResponse struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// createTenantRequest adalah body untuk POST /v1/tenants.
type createTenantRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// patchTenantRequest adalah body untuk PATCH /v1/tenants/{slug}.
//
// Field-nya pointer, bukan nilai biasa. Itu yang membedakan "field tidak
// dikirim" dari "field dikirim bernilai kosong": tanpa pointer, body {}
// akan tampak sama dengan {"name":""} dan diam-diam mengosongkan kolom.
type patchTenantRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

// listResponse membungkus satu halaman hasil beserta cursor berikutnya.
// Generic supaya bentuk response daftar seragam untuk semua resource.
type listResponse[T any] struct {
	Items []T `json:"items"`
	// NextCursor kosong berarti tidak ada halaman lagi.
	NextCursor string `json:"next_cursor,omitempty"`
}

// newTenantResponse memetakan entitas domain ke bentuk response.
func newTenantResponse(t domain.Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID.String(),
		Slug:      t.Slug,
		Name:      t.Name,
		Status:    string(t.Status),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// decodeJSON membaca body permintaan menjadi dst.
//
// Semua kegagalan dikembalikan sebagai domain.ErrInvalid supaya berakhir
// sebagai 400, bukan 500: body yang rusak adalah kesalahan klien.
//
// DisallowUnknownFields dinyalakan agar salah ketik nama field — misalnya
// "nmae" — ditolak dengan jelas, bukan diterima lalu diam-diam diabaikan
// sehingga klien mengira permintaannya berhasil.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return fmt.Errorf("%w: request body exceeds 1 MiB", domain.ErrInvalid)
		case errors.Is(err, io.EOF):
			return fmt.Errorf("%w: request body is empty", domain.ErrInvalid)
		default:
			return fmt.Errorf("%w: request body is not valid JSON: %s", domain.ErrInvalid, err.Error())
		}
	}

	// Body harus memuat tepat satu dokumen JSON. Sisa data setelahnya
	// biasanya berarti klien mengirim dua objek berturut-turut.
	if dec.More() {
		return fmt.Errorf("%w: request body must contain exactly one JSON object", domain.ErrInvalid)
	}
	return nil
}
```

- [ ] **Step 6: Write the failing tenant handler tests**

Create `internal/api/tenant_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// fakeTenantStore adalah implementasi store.TenantStore di memori.
//
// Handler diuji dengan fake, bukan database sungguhan, karena yang diuji
// di sini adalah status code, bentuk envelope, dan pemetaan DTO — bukan
// SQL. SQL sudah diuji terhadap PostgreSQL asli di Task 4.
type fakeTenantStore struct {
	bySlug map[string]domain.Tenant
	// forceErr, bila diisi, dikembalikan oleh setiap method. Dipakai untuk
	// menguji jalur kegagalan tanpa harus memalsukan kondisi database.
	forceErr error
}

func newFakeTenantStore() *fakeTenantStore {
	return &fakeTenantStore{bySlug: map[string]domain.Tenant{}}
}

var _ store.TenantStore = (*fakeTenantStore)(nil)

func (f *fakeTenantStore) Create(_ context.Context, t *domain.Tenant) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	if _, exists := f.bySlug[t.Slug]; exists {
		return domain.ErrAlreadyExists
	}
	f.bySlug[t.Slug] = *t
	return nil
}

func (f *fakeTenantStore) GetBySlug(_ context.Context, slug string) (*domain.Tenant, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	t, ok := f.bySlug[slug]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &t, nil
}

func (f *fakeTenantStore) List(_ context.Context, _ store.TenantFilter) ([]domain.Tenant, string, error) {
	if f.forceErr != nil {
		return nil, "", f.forceErr
	}
	items := make([]domain.Tenant, 0, len(f.bySlug))
	for _, t := range f.bySlug {
		items = append(items, t)
	}
	return items, "", nil
}

func (f *fakeTenantStore) Update(_ context.Context, t *domain.Tenant) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	if _, ok := f.bySlug[t.Slug]; !ok {
		return domain.ErrNotFound
	}
	f.bySlug[t.Slug] = *t
	return nil
}

func (f *fakeTenantStore) Delete(_ context.Context, slug string) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	if _, ok := f.bySlug[slug]; !ok {
		return domain.ErrNotFound
	}
	delete(f.bySlug, slug)
	return nil
}

// seedTenant memasukkan satu tenant langsung ke fake store.
func seedTenant(f *fakeTenantStore, slug string) domain.Tenant {
	t := domain.Tenant{
		ID: uuid.New(), Slug: slug, Name: "Seeded", Status: domain.TenantActive,
	}
	f.bySlug[slug] = t
	return t
}

// doRequest menjalankan satu permintaan terhadap mux yang diberikan.
func doRequest(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeErrorBody membaca envelope error dari response.
func decodeErrorBody(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not a valid error envelope: %v\n%s", err, rec.Body.String())
	}
	return body
}
```

The file above defines only helpers and the fake store; the tests that use them are added in the next step.

- [ ] **Step 7: Write the failing tenant handler tests**

Append to `internal/api/tenant_test.go`:

```go
// newTenantMux merangkai handler tenant ke dalam mux, persis seperti yang
// dilakukan router sungguhan di Task 8.
func newTenantMux(s store.TenantStore) http.Handler {
	h := NewTenantHandler(s)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tenants", h.Create)
	mux.HandleFunc("GET /v1/tenants", h.List)
	mux.HandleFunc("GET /v1/tenants/{slug}", h.Get)
	mux.HandleFunc("PATCH /v1/tenants/{slug}", h.Patch)
	mux.HandleFunc("DELETE /v1/tenants/{slug}", h.Delete)
	return mux
}

func TestTenantHandler_Create(t *testing.T) {
	fake := newFakeTenantStore()
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPost, "/v1/tenants",
		createTenantRequest{Slug: "acme", Name: "Acme Corp"})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var got tenantResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.Slug != "acme" {
		t.Errorf("slug = %q, want %q", got.Slug, "acme")
	}
	if got.Name != "Acme Corp" {
		t.Errorf("name = %q, want %q", got.Name, "Acme Corp")
	}
	// Tenant baru selalu aktif; klien tidak boleh menentukan status saat
	// membuat.
	if got.Status != string(domain.TenantActive) {
		t.Errorf("status = %q, want %q", got.Status, domain.TenantActive)
	}
	if got.ID == "" {
		t.Error("response has an empty id")
	}
	if _, err := uuid.Parse(got.ID); err != nil {
		t.Errorf("id %q is not a valid UUID: %v", got.ID, err)
	}
	if got.CreatedAt.IsZero() {
		t.Error("response has a zero created_at")
	}
}

func TestTenantHandler_CreateDuplicateReturnsConflict(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPost, "/v1/tenants",
		createTenantRequest{Slug: "acme", Name: "Another Acme"})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorBody(t, rec).Error.Code; code != codeAlreadyExists {
		t.Errorf("error.code = %q, want %q", code, codeAlreadyExists)
	}
}

func TestTenantHandler_CreateValidatesInput(t *testing.T) {
	tests := []struct {
		name      string
		body      createTenantRequest
		wantField string
	}{
		{"slug too short", createTenantRequest{Slug: "ab", Name: "Fine"}, "slug"},
		{"slug uppercase", createTenantRequest{Slug: "Acme", Name: "Fine"}, "slug"},
		{"slug with space", createTenantRequest{Slug: "ac me", Name: "Fine"}, "slug"},
		{"name empty", createTenantRequest{Slug: "acme", Name: ""}, "name"},
		{"name blank", createTenantRequest{Slug: "acme", Name: "   "}, "name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTenantMux(newFakeTenantStore())

			rec := doRequest(t, mux, http.MethodPost, "/v1/tenants", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			body := decodeErrorBody(t, rec)
			if body.Error.Code != codeInvalid {
				t.Errorf("error.code = %q, want %q", body.Error.Code, codeInvalid)
			}
			if len(body.Error.Details) != 1 {
				t.Fatalf("len(details) = %d, want 1: %+v", len(body.Error.Details), body.Error)
			}
			if body.Error.Details[0].Field != tt.wantField {
				t.Errorf("details[0].field = %q, want %q", body.Error.Details[0].Field, tt.wantField)
			}
		})
	}
}

// Review Focus #1: body JSON rusak harus 400 dengan envelope standar,
// bukan panic dan bukan 500.
func TestTenantHandler_CreateRejectsMalformedBody(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"truncated json", `{"slug":"acme"`},
		{"not json at all", `this is not json`},
		{"empty body", ``},
		{"json array instead of object", `["acme"]`},
		{"unknown field", `{"slug":"acme","name":"Acme","admin":true}`},
		{"two objects", `{"slug":"acme","name":"A"}{"slug":"b","name":"B"}`},
		{"wrong field type", `{"slug":123,"name":"Acme"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := newTenantMux(newFakeTenantStore())

			rec := doRequest(t, mux, http.MethodPost, "/v1/tenants", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			if code := decodeErrorBody(t, rec).Error.Code; code != codeInvalid {
				t.Errorf("error.code = %q, want %q", code, codeInvalid)
			}
		})
	}
}

func TestTenantHandler_Get(t *testing.T) {
	fake := newFakeTenantStore()
	want := seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got tenantResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.ID != want.ID.String() {
		t.Errorf("id = %q, want %q", got.ID, want.ID)
	}
}

func TestTenantHandler_GetMissingReturnsNotFound(t *testing.T) {
	mux := newTenantMux(newFakeTenantStore())

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/absent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if code := decodeErrorBody(t, rec).Error.Code; code != codeNotFound {
		t.Errorf("error.code = %q, want %q", code, codeNotFound)
	}
}

func TestTenantHandler_List(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	seedTenant(fake, "globex")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got listResponse[tenantResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(got.Items))
	}
}

// Daftar kosong harus menjadi [] di JSON, bukan null. Klien yang melakukan
// iterasi atas null akan gagal di banyak bahasa.
func TestTenantHandler_ListEmptyReturnsEmptyArray(t *testing.T) {
	mux := newTenantMux(newFakeTenantStore())

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"items":[]`)) {
		t.Errorf("empty list body = %s, want items to be an empty array", rec.Body.String())
	}
}

// Review Focus #5: PATCH hanya mengubah field yang dikirim.
func TestTenantHandler_PatchOnlyChangesSuppliedFields(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPatch, "/v1/tenants/acme", `{"name":"Renamed"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got tenantResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.Name != "Renamed" {
		t.Errorf("name = %q, want %q", got.Name, "Renamed")
	}
	// Status tidak dikirim, jadi harus tetap seperti semula.
	if got.Status != string(domain.TenantActive) {
		t.Errorf("status = %q, want it unchanged at %q", got.Status, domain.TenantActive)
	}
	if got.Slug != "acme" {
		t.Errorf("slug = %q, want it unchanged", got.Slug)
	}
}

func TestTenantHandler_PatchEmptyBodyChangesNothing(t *testing.T) {
	fake := newFakeTenantStore()
	before := seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPatch, "/v1/tenants/acme", `{}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	after := fake.bySlug["acme"]
	if after.Name != before.Name {
		t.Errorf("name changed from %q to %q on an empty patch", before.Name, after.Name)
	}
	if after.Status != before.Status {
		t.Errorf("status changed from %q to %q on an empty patch", before.Status, after.Status)
	}
}

func TestTenantHandler_PatchValidatesStatus(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPatch, "/v1/tenants/acme", `{"status":"deleted"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	body := decodeErrorBody(t, rec)
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "status" {
		t.Errorf("details = %+v, want one entry for field \"status\"", body.Error.Details)
	}
}

// Slug adalah identitas di URL dan tidak boleh diubah lewat PATCH.
func TestTenantHandler_PatchRejectsSlugChange(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodPatch, "/v1/tenants/acme", `{"slug":"renamed"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an attempt to change slug: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantHandler_PatchMissingReturnsNotFound(t *testing.T) {
	mux := newTenantMux(newFakeTenantStore())

	rec := doRequest(t, mux, http.MethodPatch, "/v1/tenants/absent", `{"name":"X"}`)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestTenantHandler_Delete(t *testing.T) {
	fake := newFakeTenantStore()
	seedTenant(fake, "acme")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodDelete, "/v1/tenants/acme", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response has a body: %s", rec.Body.String())
	}
	if _, exists := fake.bySlug["acme"]; exists {
		t.Error("tenant still present in the store after delete")
	}
}

func TestTenantHandler_DeleteMissingReturnsNotFound(t *testing.T) {
	mux := newTenantMux(newFakeTenantStore())

	rec := doRequest(t, mux, http.MethodDelete, "/v1/tenants/absent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// Kegagalan store yang tidak dikenal harus menjadi 500 generik, dan tidak
// boleh membocorkan isi error ke klien.
func TestTenantHandler_StoreFailureReturnsInternalError(t *testing.T) {
	fake := newFakeTenantStore()
	fake.forceErr = errors.New("connection refused: 10.0.0.5:5432")
	mux := newTenantMux(fake)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme", nil)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("500 response leaked the store error: %s", rec.Body.String())
	}
}
```

- [ ] **Step 8: Run the tests to verify they fail**

Run: `go test ./internal/api/... -run TestTenantHandler`
Expected: FAIL — `undefined: NewTenantHandler`.

- [ ] **Step 9: Write the tenant handlers**

Create `internal/api/tenant.go`:

```go
package api

import (
	"net/http"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// TenantHandler melayani endpoint /v1/tenants.
//
// Field-nya adalah interface store, bukan *gorm.DB. Handler di sini tidak
// punya cara untuk mengetahui apa yang ada di baliknya — itulah sifat yang
// membuat Phase 5 bisa menyisipkan Row-Level Security tanpa menyentuh
// berkas ini.
type TenantHandler struct {
	store store.TenantStore
}

// NewTenantHandler membuat TenantHandler baru.
func NewTenantHandler(s store.TenantStore) *TenantHandler {
	return &TenantHandler{store: s}
}

// Create melayani POST /v1/tenants.
func (h *TenantHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	tenant := domain.Tenant{
		ID:   uuid.New(),
		Slug: req.Slug,
		Name: req.Name,
		// Status tidak diambil dari permintaan. Tenant baru selalu aktif;
		// menonaktifkannya adalah operasi tersendiri lewat PATCH, bukan
		// sesuatu yang bisa disisipkan saat pembuatan.
		Status: domain.TenantActive,
	}

	// Validasi dijalankan sebelum menyentuh store. Dengan begitu input
	// yang jelas salah tidak pernah menghabiskan satu pun koneksi database.
	if err := tenant.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.store.Create(r.Context(), &tenant); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newTenantResponse(tenant))
}

// Get melayani GET /v1/tenants/{slug}.
func (h *TenantHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	tenant, err := h.store.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTenantResponse(*tenant))
}

// List melayani GET /v1/tenants.
func (h *TenantHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	items, next, err := h.store.List(r.Context(), store.TenantFilter{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	// Slice dibuat dengan make, bukan var, supaya daftar kosong terkirim
	// sebagai [] dan bukan null. Klien yang melakukan iterasi atas null
	// akan gagal di banyak bahasa.
	resp := listResponse[tenantResponse]{
		Items:      make([]tenantResponse, 0, len(items)),
		NextCursor: next,
	}
	for _, t := range items {
		resp.Items = append(resp.Items, newTenantResponse(t))
	}

	writeJSON(w, http.StatusOK, resp)
}

// Patch melayani PATCH /v1/tenants/{slug}.
//
// Hanya name dan status yang bisa diubah. Slug tidak: ia identitas di URL,
// dan mengubahnya akan memutus setiap tautan serta setiap baris log yang
// sudah menyebutnya. Karena decodeJSON menolak field asing, percobaan
// mengirim "slug" otomatis berakhir sebagai 400 tanpa penanganan khusus.
func (h *TenantHandler) Patch(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var req patchTenantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	tenant, err := h.store.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, r, err)
		return
	}

	// Field berupa pointer: nil berarti klien tidak mengirimnya, sehingga
	// nilai lama dipertahankan. Tanpa pointer, body {} akan mengosongkan
	// seluruh kolom yang bisa diubah.
	if req.Name != nil {
		tenant.Name = *req.Name
	}
	if req.Status != nil {
		tenant.Status = domain.TenantStatus(*req.Status)
	}

	if err := tenant.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.store.Update(r.Context(), tenant); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTenantResponse(*tenant))
}

// Delete melayani DELETE /v1/tenants/{slug}. Agent dan kapabilitas milik
// tenant ikut terhapus lewat ON DELETE CASCADE.
func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), r.PathValue("slug")); err != nil {
		writeError(w, r, err)
		return
	}

	// 204 No Content: penghapusan berhasil dan memang tidak ada yang perlu
	// dikembalikan.
	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 10: Write the query-parameter parser**

Append to `internal/api/dto.go`:

```go
// parseLimit membaca parameter ?limit= dari query string.
//
// Kosong berarti pakai default di lapisan store. Nilai yang bukan angka
// atau negatif ditolak dengan 400, tidak diam-diam diabaikan: klien yang
// salah ketik ?limit=abc lebih baik tahu sekarang daripada bingung kenapa
// jumlah hasilnya tidak sesuai harapan.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: limit must be a number", domain.ErrInvalid)
	}
	if n < 0 {
		return 0, fmt.Errorf("%w: limit must not be negative", domain.ErrInvalid)
	}
	// Nilai di atas batas tidak ditolak; clampLimit di lapisan store yang
	// memotongnya. Permintaan yang terlalu rakus tetap dilayani, hanya saja
	// hasilnya dibatasi.
	return n, nil
}
```

- [ ] **Step 11: Run the tenant handler tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS for every test in the package, including `TestTenantHandler_CreateRejectsMalformedBody` (7 subtests), `TestTenantHandler_CreateValidatesInput` (5 subtests), `TestTenantHandler_PatchOnlyChangesSuppliedFields`, and `TestTenantHandler_StoreFailureReturnsInternalError`.

- [ ] **Step 12: Run build, vet, race, and lint**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 13: Commit**

```bash
git add internal/api/
git commit -m "feat: add error envelope, DTOs, and tenant REST handlers"
```

---
### Task 8: Agent handlers and the router

**Files:**
- Create: `internal/api/agent.go`, `internal/api/router.go`, `internal/api/agent_test.go`, `internal/api/router_test.go`
- Modify: `internal/api/dto.go` (add agent DTOs)

**Interfaces:**
- Consumes: `store.AgentStore`, `store.AgentFilter`, `store.TenantStore` (Task 3); `domain.Agent`, `domain.Capability`, `domain.AgentStatus` (Task 1); `writeError`, `writeJSON`, `decodeJSON`, `parseLimit`, `listResponse` (Task 7); middleware (Task 6).
- Produces:
  - `api.agentResponse`, `api.capabilityResponse`, `api.createAgentRequest`, `api.patchAgentRequest`
  - `api.AgentHandler` with `Create`, `Get`, `List`, `Patch`, `Delete` and `NewAgentHandler(store.TenantStore, store.AgentStore) *AgentHandler`
  - `api.Pinger` interface — `Ping(ctx context.Context) error`
  - `api.NewRouter(cfg RouterConfig) http.Handler`
  - `api.RouterConfig{Logger *slog.Logger; Tenants store.TenantStore; Agents store.AgentStore; DB Pinger}`

- [ ] **Step 1: Add the agent DTOs**

Append to `internal/api/dto.go`:

```go
// capabilityResponse adalah bentuk kapabilitas yang dikirim ke klien.
type capabilityResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// agentResponse adalah bentuk agent yang dikirim ke klien.
type agentResponse struct {
	ID           string               `json:"id"`
	TenantID     string               `json:"tenant_id"`
	Name         string               `json:"name"`
	Endpoint     string               `json:"endpoint"`
	Status       string               `json:"status"`
	Capabilities []capabilityResponse `json:"capabilities"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// capabilityRequest adalah satu elemen kapabilitas dalam permintaan create.
type capabilityRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// createAgentRequest adalah body untuk POST /v1/tenants/{slug}/agents.
//
// Status tidak ada di sini dengan sengaja: agent yang baru didaftarkan
// selalu berstatus unknown karena control plane belum pernah mendengar
// kabar apa pun darinya. Heartbeat yang mengubah status baru ada di Phase 2.
type createAgentRequest struct {
	Name         string              `json:"name"`
	Endpoint     string              `json:"endpoint"`
	Capabilities []capabilityRequest `json:"capabilities"`
}

// patchAgentRequest adalah body untuk PATCH pada satu agent. Pointer
// membedakan "tidak dikirim" dari "dikirim kosong".
type patchAgentRequest struct {
	Endpoint *string `json:"endpoint"`
	Status   *string `json:"status"`
}

// newAgentResponse memetakan entitas domain ke bentuk response.
func newAgentResponse(a domain.Agent) agentResponse {
	caps := make([]capabilityResponse, 0, len(a.Capabilities))
	for _, c := range a.Capabilities {
		caps = append(caps, capabilityResponse{Name: c.Name, Version: c.Version})
	}
	return agentResponse{
		ID:           a.ID.String(),
		TenantID:     a.TenantID.String(),
		Name:         a.Name,
		Endpoint:     a.Endpoint,
		Status:       string(a.Status),
		Capabilities: caps,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}
```

- [ ] **Step 2: Write the failing agent handler tests**

Create `internal/api/agent_test.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// fakeAgentStore adalah implementasi store.AgentStore di memori.
// Kuncinya sengaja memakai tenantID agar fake ini ikut menegakkan
// pembatasan tenant, bukan hanya menyimpan data.
type fakeAgentStore struct {
	byTenantAndName map[string]domain.Agent
	forceErr        error
}

func newFakeAgentStore() *fakeAgentStore {
	return &fakeAgentStore{byTenantAndName: map[string]domain.Agent{}}
}

var _ store.AgentStore = (*fakeAgentStore)(nil)

func agentKey(tenantID uuid.UUID, name string) string {
	return tenantID.String() + "/" + name
}

func (f *fakeAgentStore) Create(_ context.Context, a *domain.Agent) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	key := agentKey(a.TenantID, a.Name)
	if _, exists := f.byTenantAndName[key]; exists {
		return domain.ErrAlreadyExists
	}
	f.byTenantAndName[key] = *a
	return nil
}

func (f *fakeAgentStore) GetByName(_ context.Context, tenantID uuid.UUID, name string) (*domain.Agent, error) {
	if f.forceErr != nil {
		return nil, f.forceErr
	}
	a, ok := f.byTenantAndName[agentKey(tenantID, name)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &a, nil
}

func (f *fakeAgentStore) List(_ context.Context, tenantID uuid.UUID, filter store.AgentFilter) ([]domain.Agent, string, error) {
	if f.forceErr != nil {
		return nil, "", f.forceErr
	}
	items := make([]domain.Agent, 0)
	for _, a := range f.byTenantAndName {
		if a.TenantID != tenantID {
			continue
		}
		if filter.Status != "" && string(a.Status) != filter.Status {
			continue
		}
		if filter.Capability != "" && !hasCapability(a, filter.Capability) {
			continue
		}
		items = append(items, a)
	}
	return items, "", nil
}

func hasCapability(a domain.Agent, name string) bool {
	for _, c := range a.Capabilities {
		if c.Name == name {
			return true
		}
	}
	return false
}

func (f *fakeAgentStore) Update(_ context.Context, a *domain.Agent) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	key := agentKey(a.TenantID, a.Name)
	if _, ok := f.byTenantAndName[key]; !ok {
		return domain.ErrNotFound
	}
	f.byTenantAndName[key] = *a
	return nil
}

func (f *fakeAgentStore) Delete(_ context.Context, tenantID uuid.UUID, name string) error {
	if f.forceErr != nil {
		return f.forceErr
	}
	key := agentKey(tenantID, name)
	if _, ok := f.byTenantAndName[key]; !ok {
		return domain.ErrNotFound
	}
	delete(f.byTenantAndName, key)
	return nil
}

// newAgentMux merangkai handler agent persis seperti router sungguhan.
func newAgentMux(ts store.TenantStore, as store.AgentStore) http.Handler {
	h := NewAgentHandler(ts, as)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/tenants/{slug}/agents", h.Create)
	mux.HandleFunc("GET /v1/tenants/{slug}/agents", h.List)
	mux.HandleFunc("GET /v1/tenants/{slug}/agents/{name}", h.Get)
	mux.HandleFunc("PATCH /v1/tenants/{slug}/agents/{name}", h.Patch)
	mux.HandleFunc("DELETE /v1/tenants/{slug}/agents/{name}", h.Delete)
	return mux
}

// seedAgent memasukkan satu agent langsung ke fake store.
func seedAgent(f *fakeAgentStore, tenantID uuid.UUID, name string, caps ...domain.Capability) domain.Agent {
	a := domain.Agent{
		ID: uuid.New(), TenantID: tenantID, Name: name,
		Endpoint: "dns:///seeded:9090", Status: domain.AgentUnknown,
		Capabilities: caps,
	}
	f.byTenantAndName[agentKey(tenantID, name)] = a
	return a
}

func TestAgentHandler_Create(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodPost, "/v1/tenants/acme/agents", createAgentRequest{
		Name:     "security-agent",
		Endpoint: "dns:///security:9090",
		Capabilities: []capabilityRequest{
			{Name: "security-review", Version: "v1"},
		},
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var got agentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.Name != "security-agent" {
		t.Errorf("name = %q, want %q", got.Name, "security-agent")
	}
	if got.TenantID != tenant.ID.String() {
		t.Errorf("tenant_id = %q, want %q", got.TenantID, tenant.ID)
	}
	// Agent baru selalu unknown: belum ada sinyal kesehatan apa pun.
	if got.Status != string(domain.AgentUnknown) {
		t.Errorf("status = %q, want %q", got.Status, domain.AgentUnknown)
	}
	if len(got.Capabilities) != 1 {
		t.Fatalf("len(capabilities) = %d, want 1", len(got.Capabilities))
	}
	if got.Capabilities[0].Name != "security-review" {
		t.Errorf("capabilities[0].name = %q, want %q", got.Capabilities[0].Name, "security-review")
	}
}

// Review Focus #3: tenant yang tidak ada harus 404, bukan daftar kosong
// yang menyiratkan tenant itu ada.
func TestAgentHandler_UnknownTenantReturnsNotFound(t *testing.T) {
	tenants := newFakeTenantStore()
	agents := newFakeAgentStore()
	mux := newAgentMux(tenants, agents)

	tests := []struct {
		name   string
		method string
		path   string
		body   any
	}{
		{"create", http.MethodPost, "/v1/tenants/ghost/agents",
			createAgentRequest{Name: "a", Endpoint: "dns:///a:1", Capabilities: nil}},
		{"list", http.MethodGet, "/v1/tenants/ghost/agents", nil},
		{"get", http.MethodGet, "/v1/tenants/ghost/agents/some-agent", nil},
		{"patch", http.MethodPatch, "/v1/tenants/ghost/agents/some-agent", `{"status":"healthy"}`},
		{"delete", http.MethodDelete, "/v1/tenants/ghost/agents/some-agent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, mux, tt.method, tt.path, tt.body)

			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 for an unknown tenant: %s", rec.Code, rec.Body.String())
			}
			if code := decodeErrorBody(t, rec).Error.Code; code != codeNotFound {
				t.Errorf("error.code = %q, want %q", code, codeNotFound)
			}
		})
	}
}

func TestAgentHandler_CreateValidatesInput(t *testing.T) {
	tests := []struct {
		name      string
		body      createAgentRequest
		wantField string
	}{
		{"name empty", createAgentRequest{Name: "", Endpoint: "dns:///a:1"}, "name"},
		{"name uppercase", createAgentRequest{Name: "Security", Endpoint: "dns:///a:1"}, "name"},
		{"endpoint empty", createAgentRequest{Name: "agent", Endpoint: ""}, "endpoint"},
		{"capability name invalid", createAgentRequest{
			Name: "agent", Endpoint: "dns:///a:1",
			Capabilities: []capabilityRequest{{Name: "Bad Name", Version: "v1"}},
		}, "capabilities[0].name"},
		{"capability version invalid", createAgentRequest{
			Name: "agent", Endpoint: "dns:///a:1",
			Capabilities: []capabilityRequest{{Name: "review", Version: "1.0"}},
		}, "capabilities[0].version"},
		{"duplicate capabilities", createAgentRequest{
			Name: "agent", Endpoint: "dns:///a:1",
			Capabilities: []capabilityRequest{
				{Name: "review", Version: "v1"},
				{Name: "review", Version: "v1"},
			},
		}, "capabilities[1]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenants := newFakeTenantStore()
			seedTenant(tenants, "acme")
			mux := newAgentMux(tenants, newFakeAgentStore())

			rec := doRequest(t, mux, http.MethodPost, "/v1/tenants/acme/agents", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
			body := decodeErrorBody(t, rec)
			if len(body.Error.Details) != 1 {
				t.Fatalf("len(details) = %d, want 1: %+v", len(body.Error.Details), body.Error)
			}
			if body.Error.Details[0].Field != tt.wantField {
				t.Errorf("details[0].field = %q, want %q", body.Error.Details[0].Field, tt.wantField)
			}
		})
	}
}

func TestAgentHandler_CreateDuplicateReturnsConflict(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "security-agent")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodPost, "/v1/tenants/acme/agents", createAgentRequest{
		Name: "security-agent", Endpoint: "dns:///other:9090",
	})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/api/... -run TestAgentHandler`
Expected: FAIL — `undefined: NewAgentHandler`.

- [ ] **Step 4: Write the remaining agent handler tests**

Append to `internal/api/agent_test.go`:

```go
func TestAgentHandler_Get(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	want := seedAgent(agents, tenant.ID, "security-agent",
		domain.Capability{Name: "security-review", Version: "v1"})
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme/agents/security-agent", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got agentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.ID != want.ID.String() {
		t.Errorf("id = %q, want %q", got.ID, want.ID)
	}
	if len(got.Capabilities) != 1 {
		t.Errorf("len(capabilities) = %d, want 1", len(got.Capabilities))
	}
}

func TestAgentHandler_GetMissingReturnsNotFound(t *testing.T) {
	tenants := newFakeTenantStore()
	seedTenant(tenants, "acme")
	mux := newAgentMux(tenants, newFakeAgentStore())

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme/agents/absent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

// Satu tenant tidak boleh bisa membaca agent milik tenant lain lewat URL-nya
// sendiri. Ini properti isolasi yang sama seperti di store, diuji ulang di
// lapisan HTTP karena di sinilah slug tenant diterjemahkan menjadi ID.
func TestAgentHandler_GetIsScopedToTenantInPath(t *testing.T) {
	tenants := newFakeTenantStore()
	tenantA := seedTenant(tenants, "acme")
	seedTenant(tenants, "globex")
	agents := newFakeAgentStore()
	seedAgent(agents, tenantA.ID, "secret-agent")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/globex/agents/secret-agent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when reading another tenant's agent: %s", rec.Code, rec.Body.String())
	}
}

func TestAgentHandler_List(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "a-one")
	seedAgent(agents, tenant.ID, "a-two")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme/agents", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got listResponse[agentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(got.Items) != 2 {
		t.Errorf("len(items) = %d, want 2", len(got.Items))
	}
}

// Query yang akan dipakai router Phase 3, lewat HTTP.
func TestAgentHandler_ListFiltersByCapability(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "reviewer",
		domain.Capability{Name: "security-review", Version: "v1"})
	seedAgent(agents, tenant.ID, "planner",
		domain.Capability{Name: "plan-work", Version: "v1"})
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodGet,
		"/v1/tenants/acme/agents?capability=security-review", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got listResponse[agentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1: %+v", len(got.Items), got.Items)
	}
	if got.Items[0].Name != "reviewer" {
		t.Errorf("items[0].name = %q, want %q", got.Items[0].Name, "reviewer")
	}
}

func TestAgentHandler_ListFiltersByStatus(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()

	healthy := seedAgent(agents, tenant.ID, "healthy-agent")
	healthy.Status = domain.AgentHealthy
	agents.byTenantAndName[agentKey(tenant.ID, "healthy-agent")] = healthy
	seedAgent(agents, tenant.ID, "unknown-agent")

	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme/agents?status=healthy", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got listResponse[agentResponse]
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1: %+v", len(got.Items), got.Items)
	}
	if got.Items[0].Name != "healthy-agent" {
		t.Errorf("items[0].name = %q, want %q", got.Items[0].Name, "healthy-agent")
	}
}

// Review Focus #2: parameter query yang tidak masuk akal harus 400, bukan
// diabaikan diam-diam dan bukan 500.
func TestAgentHandler_ListRejectsBadQueryParameters(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"non numeric limit", "?limit=abc"},
		{"negative limit", "?limit=-5"},
		{"empty limit value", "?limit="},
		{"float limit", "?limit=1.5"},
		{"unknown status", "?status=degraded"},
		{"malformed cursor", "?cursor=!!!not-base64!!!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tenants := newFakeTenantStore()
			seedTenant(tenants, "acme")
			mux := newAgentMux(tenants, newFakeAgentStore())

			rec := doRequest(t, mux, http.MethodGet, "/v1/tenants/acme/agents"+tt.query, nil)

			// ?limit= yang kosong sah: artinya "pakai default".
			if tt.query == "?limit=" {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 for an empty limit: %s", rec.Code, rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for %s: %s", rec.Code, tt.query, rec.Body.String())
			}
			if code := decodeErrorBody(t, rec).Error.Code; code != codeInvalid {
				t.Errorf("error.code = %q, want %q", code, codeInvalid)
			}
		})
	}
}

// Review Focus #5 untuk agent: PATCH hanya menyentuh field yang dikirim.
func TestAgentHandler_PatchOnlyChangesSuppliedFields(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "security-agent",
		domain.Capability{Name: "security-review", Version: "v1"})
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodPatch,
		"/v1/tenants/acme/agents/security-agent", `{"status":"healthy"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var got agentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if got.Status != string(domain.AgentHealthy) {
		t.Errorf("status = %q, want %q", got.Status, domain.AgentHealthy)
	}
	// Endpoint tidak dikirim, jadi tidak boleh berubah.
	if got.Endpoint != "dns:///seeded:9090" {
		t.Errorf("endpoint = %q, want it unchanged", got.Endpoint)
	}
	// Kapabilitas tidak boleh hilang gara-gara patch.
	if len(got.Capabilities) != 1 {
		t.Errorf("len(capabilities) = %d, want 1 — patch must not drop capabilities", len(got.Capabilities))
	}
}

func TestAgentHandler_PatchValidatesStatus(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "security-agent")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodPatch,
		"/v1/tenants/acme/agents/security-agent", `{"status":"degraded"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	body := decodeErrorBody(t, rec)
	if len(body.Error.Details) != 1 || body.Error.Details[0].Field != "status" {
		t.Errorf("details = %+v, want one entry for field \"status\"", body.Error.Details)
	}
}

func TestAgentHandler_PatchRejectsMalformedBody(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "security-agent")
	mux := newAgentMux(tenants, agents)

	for _, body := range []string{`{"status":`, `not json`, `{"name":"renamed"}`} {
		t.Run(body, func(t *testing.T) {
			rec := doRequest(t, mux, http.MethodPatch,
				"/v1/tenants/acme/agents/security-agent", body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 for body %q: %s", rec.Code, body, rec.Body.String())
			}
		})
	}
}

func TestAgentHandler_Delete(t *testing.T) {
	tenants := newFakeTenantStore()
	tenant := seedTenant(tenants, "acme")
	agents := newFakeAgentStore()
	seedAgent(agents, tenant.ID, "security-agent")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodDelete, "/v1/tenants/acme/agents/security-agent", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 response has a body: %s", rec.Body.String())
	}
	if _, exists := agents.byTenantAndName[agentKey(tenant.ID, "security-agent")]; exists {
		t.Error("agent still present in the store after delete")
	}
}

func TestAgentHandler_DeleteIsScopedToTenantInPath(t *testing.T) {
	tenants := newFakeTenantStore()
	tenantA := seedTenant(tenants, "acme")
	seedTenant(tenants, "globex")
	agents := newFakeAgentStore()
	seedAgent(agents, tenantA.ID, "secret-agent")
	mux := newAgentMux(tenants, agents)

	rec := doRequest(t, mux, http.MethodDelete, "/v1/tenants/globex/agents/secret-agent", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: %s", rec.Code, rec.Body.String())
	}
	if _, exists := agents.byTenantAndName[agentKey(tenantA.ID, "secret-agent")]; !exists {
		t.Error("a cross-tenant delete removed another tenant's agent")
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/api/... -run TestAgentHandler`
Expected: FAIL — still `undefined: NewAgentHandler`.

- [ ] **Step 6: Write the agent handlers**

Create `internal/api/agent.go`:

```go
package api

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// AgentHandler melayani endpoint /v1/tenants/{slug}/agents.
//
// Handler ini memegang dua store: tenant untuk menerjemahkan slug di path
// menjadi ID, dan agent untuk operasi sesungguhnya.
type AgentHandler struct {
	tenants store.TenantStore
	agents  store.AgentStore
}

// NewAgentHandler membuat AgentHandler baru.
func NewAgentHandler(tenants store.TenantStore, agents store.AgentStore) *AgentHandler {
	return &AgentHandler{tenants: tenants, agents: agents}
}

// resolveTenant menerjemahkan slug di path menjadi tenant.
//
// Setiap handler agent memanggil ini lebih dulu, sehingga tenant yang tidak
// ada selalu menghasilkan 404 — bukan daftar kosong yang menyiratkan tenant
// itu ada, dan bukan query agent yang terlanjur jalan dengan ID kosong.
//
// Di Phase 5, inilah titik tempat pemeriksaan otorisasi dipasang: satu
// tempat yang dilewati seluruh jalur agent, bukan sebaran pemeriksaan di
// setiap handler.
func (h *AgentHandler) resolveTenant(r *http.Request) (*domain.Tenant, error) {
	slug := r.PathValue("slug")
	tenant, err := h.tenants.GetBySlug(r.Context(), slug)
	if err != nil {
		return nil, fmt.Errorf("resolve tenant %q: %w", slug, err)
	}
	return tenant, nil
}

// Create melayani POST /v1/tenants/{slug}/agents.
func (h *AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var req createAgentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	agentID := uuid.New()
	caps := make([]domain.Capability, 0, len(req.Capabilities))
	for _, c := range req.Capabilities {
		caps = append(caps, domain.Capability{
			ID:      uuid.New(),
			AgentID: agentID,
			Name:    c.Name,
			Version: c.Version,
		})
	}

	agent := domain.Agent{
		ID:       agentID,
		TenantID: tenant.ID,
		Name:     req.Name,
		Endpoint: req.Endpoint,
		// Status tidak diambil dari permintaan. Control plane belum pernah
		// mendengar kabar dari agent ini, jadi unknown adalah satu-satunya
		// nilai yang jujur. Phase 2 yang akan mengubahnya lewat heartbeat.
		Status:       domain.AgentUnknown,
		Capabilities: caps,
	}

	if err := agent.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.agents.Create(r.Context(), &agent); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newAgentResponse(agent))
}

// Get melayani GET /v1/tenants/{slug}/agents/{name}.
func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	agent, err := h.agents.GetByName(r.Context(), tenant.ID, r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newAgentResponse(*agent))
}

// List melayani GET /v1/tenants/{slug}/agents.
//
// Filter ?capability= di sini adalah query yang persis akan dipakai router
// di Phase 3. Menyediakannya sekarang lewat REST berarti perilakunya sudah
// bisa diuji manusia jauh sebelum ada router.
func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	query := r.URL.Query()

	limit, err := parseLimit(query.Get("limit"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	status := query.Get("status")
	if status != "" && !domain.AgentStatus(status).Valid() {
		writeError(w, r, domain.ValidationError{
			Field: "status",
			Issue: "must be one of: unknown, healthy, unhealthy",
		})
		return
	}

	items, next, err := h.agents.List(r.Context(), tenant.ID, store.AgentFilter{
		Capability: query.Get("capability"),
		Status:     status,
		Limit:      limit,
		Cursor:     query.Get("cursor"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	resp := listResponse[agentResponse]{
		Items:      make([]agentResponse, 0, len(items)),
		NextCursor: next,
	}
	for _, a := range items {
		resp.Items = append(resp.Items, newAgentResponse(a))
	}

	writeJSON(w, http.StatusOK, resp)
}

// Patch melayani PATCH /v1/tenants/{slug}/agents/{name}.
//
// Hanya endpoint dan status yang bisa diubah. Nama adalah identitas di URL;
// kapabilitas dikelola lewat create dan delete agent, bukan lewat patch —
// mengganti seluruh daftar kapabilitas lewat satu field tersembunyi akan
// menjadi kejutan yang mahal.
func (h *AgentHandler) Patch(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	var req patchAgentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	agent, err := h.agents.GetByName(r.Context(), tenant.ID, r.PathValue("name"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	if req.Endpoint != nil {
		agent.Endpoint = *req.Endpoint
	}
	if req.Status != nil {
		agent.Status = domain.AgentStatus(*req.Status)
	}

	if err := agent.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.agents.Update(r.Context(), agent); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newAgentResponse(*agent))
}

// Delete melayani DELETE /v1/tenants/{slug}/agents/{name}.
func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenant, err := h.resolveTenant(r)
	if err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.agents.Delete(r.Context(), tenant.ID, r.PathValue("name")); err != nil {
		writeError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 7: Run the agent handler tests to verify they pass**

Run: `go test ./internal/api/... -run TestAgentHandler -v`
Expected: PASS for all fifteen tests, including `UnknownTenantReturnsNotFound` (5 subtests), `ListRejectsBadQueryParameters` (6 subtests), and `CreateValidatesInput` (6 subtests).

- [ ] **Step 8: Write the failing router tests**

Create `internal/api/router_test.go`:

```go
package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentmesh/internal/observability"
)

// fakePinger memalsukan pemeriksaan kesehatan database.
type fakePinger struct {
	err error
}

func (f fakePinger) Ping(context.Context) error { return f.err }

var _ Pinger = fakePinger{}

func newTestRouter(ping error) http.Handler {
	return NewRouter(RouterConfig{
		Logger:  observability.NewLogger(&bytes.Buffer{}, "test", "error"),
		Tenants: newFakeTenantStore(),
		Agents:  newFakeAgentStore(),
		DB:      fakePinger{err: ping},
	})
}

// Liveness tidak menyentuh database: ia hanya menjawab "proses ini hidup".
func TestRouter_HealthzIgnoresDatabase(t *testing.T) {
	router := newTestRouter(errors.New("database is down"))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even with a failing database: %s", rec.Code, rec.Body.String())
	}
}

// Readiness justru harus memeriksa database: proses yang kehilangan
// database tidak bisa melayani permintaan, meski ia masih hidup.
func TestRouter_ReadyzReflectsDatabaseHealth(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantStatus int
	}{
		{"database reachable", nil, http.StatusOK},
		{"database unreachable", errors.New("connection refused"), http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(tt.pingErr)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
		})
	}
}

// Detail kegagalan database tidak boleh bocor lewat endpoint yang bisa
// diakses tanpa autentikasi.
func TestRouter_ReadyzDoesNotLeakDatabaseDetail(t *testing.T) {
	router := newTestRouter(errors.New("dial tcp 10.0.0.5:5432: connection refused"))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if body := rec.Body.String(); bytes.Contains([]byte(body), []byte("10.0.0.5")) {
		t.Errorf("/readyz leaked the database address: %s", body)
	}
}

func TestRouter_RoutesAreWired(t *testing.T) {
	router := newTestRouter(nil)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"list tenants", http.MethodGet, "/v1/tenants", http.StatusOK},
		{"get unknown tenant", http.MethodGet, "/v1/tenants/ghost", http.StatusNotFound},
		{"list agents of unknown tenant", http.MethodGet, "/v1/tenants/ghost/agents", http.StatusNotFound},
		{"unknown path", http.MethodGet, "/v1/nope", http.StatusNotFound},
		{"wrong method on tenants", http.MethodPut, "/v1/tenants", http.StatusMethodNotAllowed},
		{"healthz", http.MethodGet, "/healthz", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// Middleware harus benar-benar terpasang pada router sungguhan, bukan
// hanya teruji sendiri-sendiri.
func TestRouter_AppliesMiddleware(t *testing.T) {
	router := newTestRouter(nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tenants", nil))

	if rec.Header().Get(RequestIDHeader) == "" {
		t.Error("response is missing the request id header — middleware is not wired")
	}
}
```

- [ ] **Step 9: Run the tests to verify they fail**

Run: `go test ./internal/api/... -run TestRouter`
Expected: FAIL — `undefined: NewRouter`, `undefined: RouterConfig`, `undefined: Pinger`.

- [ ] **Step 10: Write the router**

Create `internal/api/router.go`:

```go
package api

import (
	"context"
	"log/slog"
	"net/http"

	"agentmesh/internal/store"
)

// Pinger adalah apa pun yang bisa dipastikan masih terjangkau.
//
// Interface sekecil ini, bukan *gorm.DB langsung, agar package api tetap
// tidak mengenal GORM sama sekali — dan agar test bisa memalsukan database
// yang sedang mati tanpa mematikan database sungguhan.
type Pinger interface {
	Ping(ctx context.Context) error
}

// RouterConfig berisi segala yang dibutuhkan router.
type RouterConfig struct {
	Logger  *slog.Logger
	Tenants store.TenantStore
	Agents  store.AgentStore
	DB      Pinger
}

// NewRouter merangkai seluruh rute dan middleware control plane.
func NewRouter(cfg RouterConfig) http.Handler {
	tenantHandler := NewTenantHandler(cfg.Tenants)
	agentHandler := NewAgentHandler(cfg.Tenants, cfg.Agents)

	mux := http.NewServeMux()

	// Endpoint kesehatan. Keduanya sengaja dibedakan.
	//
	// healthz menjawab "proses ini hidup, jangan dibunuh". Ia tidak
	// menyentuh database: control plane yang kehilangan database tetap
	// proses yang sehat, dan membunuh lalu menjalankannya ulang tidak akan
	// menghidupkan database.
	//
	// readyz menjawab "saya bisa melayani permintaan". Ia memeriksa
	// database, karena tanpa database tidak ada satu pun endpoint /v1 yang
	// bisa bekerja. Load balancer memakai sinyal ini untuk berhenti
	// mengirim traffic tanpa ikut me-restart proses.
	//
	// Menyatukan keduanya adalah cara umum sebuah layanan masuk ke restart
	// loop saat database berkedip sesaat.
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(cfg.DB))

	// Tenant.
	mux.HandleFunc("POST /v1/tenants", tenantHandler.Create)
	mux.HandleFunc("GET /v1/tenants", tenantHandler.List)
	mux.HandleFunc("GET /v1/tenants/{slug}", tenantHandler.Get)
	mux.HandleFunc("PATCH /v1/tenants/{slug}", tenantHandler.Patch)
	mux.HandleFunc("DELETE /v1/tenants/{slug}", tenantHandler.Delete)

	// Agent, selalu bersarang di bawah tenant-nya. Segmen {slug} di path
	// inilah yang akan menjadi batas otorisasi di Phase 5.
	mux.HandleFunc("POST /v1/tenants/{slug}/agents", agentHandler.Create)
	mux.HandleFunc("GET /v1/tenants/{slug}/agents", agentHandler.List)
	mux.HandleFunc("GET /v1/tenants/{slug}/agents/{name}", agentHandler.Get)
	mux.HandleFunc("PATCH /v1/tenants/{slug}/agents/{name}", agentHandler.Patch)
	mux.HandleFunc("DELETE /v1/tenants/{slug}/agents/{name}", agentHandler.Delete)

	// Urutan middleware penting. Recovery paling luar supaya panic di
	// middleware mana pun ikut tertangkap; request id sebelum logging
	// supaya setiap baris log sudah membawa id-nya.
	return Chain(mux,
		WithRecovery(cfg.Logger),
		WithRequestID(),
		WithLogging(cfg.Logger),
	)
}

// handleHealthz adalah probe liveness.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleReadyz adalah probe readiness: ia memeriksa dependensi.
func handleReadyz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context()); err != nil {
			// Penyebabnya dicatat di log, tidak dikirim ke klien: endpoint
			// ini bisa diakses tanpa autentikasi, dan alamat atau kredensial
			// database tidak boleh bocor lewatnya.
			LoggerFrom(r.Context()).Error("readiness check failed", "error", err.Error())

			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"reason": "database is not reachable",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
```

- [ ] **Step 11: Run the router tests to verify they pass**

Run: `go test ./internal/api/... -run TestRouter -v`
Expected: PASS — `HealthzIgnoresDatabase`, `ReadyzReflectsDatabaseHealth` (2 subtests), `ReadyzDoesNotLeakDatabaseDetail`, `RoutesAreWired` (6 subtests), `AppliesMiddleware`.

- [ ] **Step 12: Run the whole API package, plus build, vet, race, and lint**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 13: Commit**

```bash
git add internal/api/
git commit -m "feat: add agent REST handlers and the control-plane router"
```

---
### Task 9: Wiring, configuration, Docker Compose, and seed data

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`, `cmd/control-plane/main.go`, `cmd/control-plane/main_test.go`, `Makefile`
- Create: `internal/store/postgres/pinger.go`, `cmd/seed/main.go`, `docker-compose.yml`, `.env.example`

**Interfaces:**
- Consumes: everything from Tasks 1–8.
- Produces:
  - `config.Config` gains `DatabaseURL string`, `MaxOpenConns int`, `MaxIdleConns int`, `ConnMaxLifetime time.Duration`
  - `postgres.NewPinger(db *gorm.DB) *Pinger` with `Ping(ctx context.Context) error`, satisfying `api.Pinger`

- [ ] **Step 1: Write the failing config tests**

Append to `internal/config/config_test.go`:

```go
func TestLoad_DatabaseURLRequired(t *testing.T) {
	for _, key := range []string{"AGENTMESH_DATABASE_URL"} {
		t.Setenv(key, "x")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q) failed: %v", key, err)
		}
	}

	_, err := Load()

	if err == nil {
		t.Fatal("Load() = nil error, want an error when AGENTMESH_DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "AGENTMESH_DATABASE_URL") {
		t.Errorf("error %q does not name the missing variable", err)
	}
}

func TestLoad_PoolDefaults(t *testing.T) {
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.MaxOpenConns != defaultMaxOpenConns {
		t.Errorf("MaxOpenConns = %d, want %d", cfg.MaxOpenConns, defaultMaxOpenConns)
	}
	if cfg.MaxIdleConns != defaultMaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", cfg.MaxIdleConns, defaultMaxIdleConns)
	}
	if cfg.ConnMaxLifetime != defaultConnMaxLifetime {
		t.Errorf("ConnMaxLifetime = %v, want %v", cfg.ConnMaxLifetime, defaultConnMaxLifetime)
	}
}

func TestLoad_RejectsInvalidPoolSettings(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"zero max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "0"},
		{"negative max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "-1"},
		{"non numeric max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "many"},
		{"negative max idle conns", "AGENTMESH_DB_MAX_IDLE_CONNS", "-1"},
		{"idle above open", "AGENTMESH_DB_MAX_IDLE_CONNS", "999"},
		{"bad lifetime", "AGENTMESH_DB_CONN_MAX_LIFETIME", "forever"},
		{"zero lifetime", "AGENTMESH_DB_CONN_MAX_LIFETIME", "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
			t.Setenv(tt.key, tt.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() = nil error, want rejection of %s=%q", tt.key, tt.value)
			}
		})
	}
}
```

Add `"strings"` to that file's import block if it is not already there.

- [ ] **Step 2: Run the config tests to verify they fail**

Run: `go test ./internal/config/...`
Expected: FAIL — `undefined: defaultMaxOpenConns`, and the existing tests now fail too because `Load` does not yet require `AGENTMESH_DATABASE_URL`.

- [ ] **Step 3: Extend the config**

In `internal/config/config.go`, extend the `Config` struct:

```go
// Config holds runtime configuration for the control-plane service.
type Config struct {
	// HTTPAddr is the address the control-plane HTTP server listens on.
	HTTPAddr string
	// LogLevel controls structured log verbosity: debug, info, warn, error.
	LogLevel string
	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish before forcing an exit.
	ShutdownTimeout time.Duration

	// DatabaseURL adalah DSN PostgreSQL. Tidak ada nilai default: tanpa
	// database, control plane tidak bisa melayani apa pun, jadi lebih baik
	// gagal saat start dengan pesan yang jelas daripada menyala lalu
	// menolak setiap permintaan.
	DatabaseURL string
	// MaxOpenConns membatasi total koneksi ke PostgreSQL.
	MaxOpenConns int
	// MaxIdleConns menahan sejumlah koneksi tetap terbuka.
	MaxIdleConns int
	// ConnMaxLifetime memaksa koneksi lama didaur ulang.
	ConnMaxLifetime time.Duration
}
```

Add the new defaults next to the existing ones:

```go
const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second

	// Nilai default pool sengaja kecil. PostgreSQL memakai satu proses per
	// koneksi, jadi batas yang kebesaran justru membebani server alih-alih
	// mempercepatnya. Angka ini cocok untuk pengembangan lokal dan aman
	// dinaikkan lewat environment variable saat dibutuhkan.
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
)
```

Extend `Load` with the new variables, before the `validate()` call:

```go
	cfg.DatabaseURL = os.Getenv("AGENTMESH_DATABASE_URL")

	cfg.MaxOpenConns = defaultMaxOpenConns
	if raw := os.Getenv("AGENTMESH_DB_MAX_OPEN_CONNS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_MAX_OPEN_CONNS %q: %w", raw, err)
		}
		cfg.MaxOpenConns = n
	}

	cfg.MaxIdleConns = defaultMaxIdleConns
	if raw := os.Getenv("AGENTMESH_DB_MAX_IDLE_CONNS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_MAX_IDLE_CONNS %q: %w", raw, err)
		}
		cfg.MaxIdleConns = n
	}

	cfg.ConnMaxLifetime = defaultConnMaxLifetime
	if raw := os.Getenv("AGENTMESH_DB_CONN_MAX_LIFETIME"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_CONN_MAX_LIFETIME %q: %w", raw, err)
		}
		cfg.ConnMaxLifetime = d
	}
```

Extend `validate()` with the new rules:

```go
	if c.DatabaseURL == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL must be set")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("AGENTMESH_DB_MAX_OPEN_CONNS must be positive, got %d", c.MaxOpenConns)
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("AGENTMESH_DB_MAX_IDLE_CONNS must not be negative, got %d", c.MaxIdleConns)
	}
	// Idle lebih besar daripada open tidak berarti apa-apa: database/sql
	// akan memangkasnya diam-diam, dan konfigurasi yang diam-diam diabaikan
	// lebih membingungkan daripada yang ditolak.
	if c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("AGENTMESH_DB_MAX_IDLE_CONNS (%d) must not exceed AGENTMESH_DB_MAX_OPEN_CONNS (%d)",
			c.MaxIdleConns, c.MaxOpenConns)
	}
	if c.ConnMaxLifetime <= 0 {
		return fmt.Errorf("AGENTMESH_DB_CONN_MAX_LIFETIME must be positive, got %s", c.ConnMaxLifetime)
	}
```

Add `"strconv"` to the file's import block.

- [ ] **Step 4: Fix the pre-existing config tests**

The Phase 0 tests `TestLoad_Defaults`, `TestLoad_OverridesFromEnv`, and the validation tests now fail because `Load` requires `AGENTMESH_DATABASE_URL`. Add this line at the top of each existing test that expects `Load()` to succeed:

```go
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
```

For `TestLoad_Defaults`, add it after the unset loop so the loop does not clear it. Do not add `AGENTMESH_DATABASE_URL` to that test's unset list.

- [ ] **Step 5: Run the config tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS for every test, old and new.

- [ ] **Step 6: Write the pinger**

Create `internal/store/postgres/pinger.go`:

```go
package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Pinger memeriksa apakah database masih terjangkau. Dipakai oleh probe
// readiness di lapisan api.
//
// Tipe kecil ini ada supaya package api tidak perlu mengenal *gorm.DB sama
// sekali: ia hanya butuh sesuatu yang punya method Ping.
type Pinger struct {
	db *gorm.DB
}

// NewPinger membuat Pinger baru.
func NewPinger(db *gorm.DB) *Pinger {
	return &Pinger{db: db}
}

// Ping memastikan database membalas.
func (p *Pinger) Ping(ctx context.Context) error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return fmt.Errorf("access underlying sql.DB: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
```

- [ ] **Step 7: Rewrite the control-plane entrypoint**

Replace the whole of `cmd/control-plane/main.go`:

```go
// cmd/control-plane/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentmesh/internal/api"
	"agentmesh/internal/config"
	"agentmesh/internal/observability"
	"agentmesh/internal/store/postgres"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observability.NewLogger(logOut, "control-plane", cfg.LogLevel)

	// Koneksi database dibuka sebelum server mulai mendengarkan. Kalau
	// database tidak terjangkau saat start, lebih baik proses gagal dengan
	// pesan jelas daripada menyala lalu menolak setiap permintaan.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 15*time.Second)
	defer cancelConnect()

	db, err := postgres.Open(connectCtx, cfg.DatabaseURL, postgres.Options{
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	logger.Info("connected to database",
		"max_open_conns", cfg.MaxOpenConns,
		"max_idle_conns", cfg.MaxIdleConns,
	)

	// Perakitan dependensi terjadi di sini dan hanya di sini. Setiap
	// lapisan menerima apa yang dibutuhkannya lewat interface, sehingga
	// tidak ada package yang mencari sendiri dependensinya lewat variabel
	// global.
	handler := api.NewRouter(api.RouterConfig{
		Logger:  logger,
		Tenants: postgres.NewTenantStore(db),
		Agents:  postgres.NewAgentStore(db),
		DB:      postgres.NewPinger(db),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting control-plane HTTP server", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("control-plane stopped cleanly")
	return nil
}
```

- [ ] **Step 8: Replace the obsolete main test**

`cmd/control-plane/main_test.go` tests `newMux`, which no longer exists — routing now lives in `internal/api` and is tested there. Replace the whole file:

```go
// cmd/control-plane/main_test.go
package main

import (
	"bytes"
	"strings"
	"testing"
)

// run harus gagal cepat dengan pesan yang menyebut variabel yang hilang,
// bukan panic dan bukan diam-diam menyala tanpa database.
func TestRun_FailsWithoutDatabaseURL(t *testing.T) {
	for _, key := range []string{"AGENTMESH_DATABASE_URL"} {
		t.Setenv(key, "x")
		if err := unsetenv(key); err != nil {
			t.Fatalf("unset %q: %v", key, err)
		}
	}

	err := run(&bytes.Buffer{})

	if err == nil {
		t.Fatal("run() = nil, want an error when AGENTMESH_DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "AGENTMESH_DATABASE_URL") {
		t.Errorf("error %q does not name the missing variable", err)
	}
}

// run harus mengembalikan error, bukan panic, ketika database tidak bisa
// dihubungi.
func TestRun_FailsWhenDatabaseUnreachable(t *testing.T) {
	// Port 1 dijamin tidak ada yang mendengarkan.
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	t.Setenv("AGENTMESH_LOG_LEVEL", "error")

	err := run(&bytes.Buffer{})

	if err == nil {
		t.Fatal("run() = nil, want an error when the database is unreachable")
	}
	if !strings.Contains(err.Error(), "connect to database") {
		t.Errorf("error %q does not explain that the database connection failed", err)
	}
}
```

Add this helper at the bottom of the same file, plus `"os"` to the imports:

```go
// unsetenv membungkus os.Unsetenv agar test di atas terbaca jelas.
func unsetenv(key string) error { return os.Unsetenv(key) }
```

- [ ] **Step 9: Write the Docker Compose file**

Create `docker-compose.yml`:

```yaml
# Stack pengembangan lokal untuk AgentMesh.
#
# Dibawa maju dari Phase 8 karena Phase 1 memperkenalkan PostgreSQL, dan
# tanpa berkas ini tidak ada cara menjalankan database secara lokal.
# Phase 8 akan menambahkan OpenTelemetry Collector dan kawan-kawannya di
# sini.

services:
  postgres:
    image: postgres:17-alpine
    container_name: agentmesh-postgres
    environment:
      POSTGRES_USER: agentmesh
      POSTGRES_PASSWORD: agentmesh
      POSTGRES_DB: agentmesh
    ports:
      - "5432:5432"
    volumes:
      - agentmesh-pgdata:/var/lib/postgresql/data
    healthcheck:
      # pg_isready penting: tanpa ini, "docker compose up -d" selesai
      # sebelum PostgreSQL benar-benar siap, dan "make migrate" yang
      # langsung dijalankan sesudahnya akan gagal.
      test: ["CMD-SHELL", "pg_isready -U agentmesh -d agentmesh"]
      interval: 2s
      timeout: 3s
      retries: 15

volumes:
  agentmesh-pgdata:
```

Create `.env.example`:

```bash
# Salin menjadi .env lalu sesuaikan bila perlu.
# Nilai di bawah cocok dengan docker-compose.yml.

AGENTMESH_DATABASE_URL=postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable
AGENTMESH_HTTP_ADDR=:8080
AGENTMESH_LOG_LEVEL=info
AGENTMESH_SHUTDOWN_TIMEOUT=10s

# Opsional: pengaturan pool koneksi.
# AGENTMESH_DB_MAX_OPEN_CONNS=25
# AGENTMESH_DB_MAX_IDLE_CONNS=5
# AGENTMESH_DB_CONN_MAX_LIFETIME=30m
```

Add `.env` to `.gitignore` if it is not already listed.

- [ ] **Step 10: Write the seed command**

Create `cmd/seed/main.go`:

```go
// Command seed memuat data demo: satu tenant "acme" dengan tiga agent.
//
// Ini adalah skenario yang dijanjikan README dan yang akan dipakai demo
// end-to-end Phase 3, sehingga datanya sudah tersedia jauh sebelum router
// ada.
//
// Operasinya idempoten: menjalankan ulang tidak menggandakan apa pun, dan
// data yang sudah ada dibiarkan. Perintah seed yang gagal pada pemanggilan
// kedua adalah gangguan kecil yang menjengkelkan selama pengembangan.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("AGENTMESH_DATABASE_URL")
	if dsn == "" {
		return errors.New("AGENTMESH_DATABASE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, dsn, postgres.Options{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	tenants := postgres.NewTenantStore(db)
	agents := postgres.NewAgentStore(db)

	tenant := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: domain.TenantActive,
	}
	if err := tenants.Create(ctx, &tenant); err != nil {
		if !errors.Is(err, domain.ErrAlreadyExists) {
			return fmt.Errorf("create tenant: %w", err)
		}
		// Sudah ada: ambil ID aslinya supaya agent di bawah menempel pada
		// tenant yang benar, bukan pada UUID yang baru saja dibuat.
		existing, getErr := tenants.GetBySlug(ctx, tenant.Slug)
		if getErr != nil {
			return fmt.Errorf("read existing tenant: %w", getErr)
		}
		tenant = *existing
		fmt.Printf("tenant %q already exists, reusing it\n", tenant.Slug)
	} else {
		fmt.Printf("created tenant %q\n", tenant.Slug)
	}

	specs := []struct {
		name         string
		endpoint     string
		capabilities []domain.Capability
	}{
		{
			name:     "planner-agent",
			endpoint: "dns:///planner-agent:9090",
			capabilities: []domain.Capability{
				{Name: "plan-work", Version: "v1"},
				{Name: "decompose-task", Version: "v1"},
			},
		},
		{
			name:     "backend-agent",
			endpoint: "dns:///backend-agent:9090",
			capabilities: []domain.Capability{
				{Name: "implement-endpoint", Version: "v1"},
				{Name: "write-migration", Version: "v1"},
			},
		},
		{
			name:     "security-agent",
			endpoint: "dns:///security-agent:9090",
			capabilities: []domain.Capability{
				{Name: "security-review", Version: "v1"},
			},
		},
	}

	for _, spec := range specs {
		agentID := uuid.New()
		caps := make([]domain.Capability, 0, len(spec.capabilities))
		for _, c := range spec.capabilities {
			caps = append(caps, domain.Capability{
				ID: uuid.New(), AgentID: agentID, Name: c.Name, Version: c.Version,
			})
		}

		agent := domain.Agent{
			ID:       agentID,
			TenantID: tenant.ID,
			Name:     spec.name,
			Endpoint: spec.endpoint,
			// Status unknown: belum ada agent sungguhan yang pernah
			// melapor. Phase 2 yang akan mengubahnya.
			Status:       domain.AgentUnknown,
			Capabilities: caps,
		}

		if err := agent.Validate(); err != nil {
			return fmt.Errorf("seed agent %q is invalid: %w", spec.name, err)
		}

		if err := agents.Create(ctx, &agent); err != nil {
			if errors.Is(err, domain.ErrAlreadyExists) {
				fmt.Printf("agent %q already exists, skipping\n", spec.name)
				continue
			}
			return fmt.Errorf("create agent %q: %w", spec.name, err)
		}
		fmt.Printf("created agent %q with %d capabilities\n", spec.name, len(caps))
	}

	return nil
}
```

- [ ] **Step 11: Wire the remaining Makefile targets**

In `Makefile`, replace the `dev`, `seed`, and `compose-down` stubs:

```makefile
.PHONY: dev
dev: ## Start local PostgreSQL via Docker Compose and wait for it to be ready
	docker compose up -d postgres
	@echo "waiting for postgres to become healthy..."
	@until [ "$$(docker inspect -f '{{.State.Health.Status}}' agentmesh-postgres 2>/dev/null)" = "healthy" ]; do \
		sleep 1; \
	done
	@echo "postgres is ready"

.PHONY: seed
seed: ## Load demo tenant/agent seed data
	AGENTMESH_DATABASE_URL="$${AGENTMESH_DATABASE_URL:-postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable}" \
		go run ./cmd/seed

.PHONY: compose-down
compose-down: ## Tear down the Docker Compose stack
	docker compose down
```

Also update the `run-control-plane` target so it carries a database URL by default:

```makefile
.PHONY: run-control-plane
run-control-plane: ## Run the control-plane service locally
	AGENTMESH_DATABASE_URL="$${AGENTMESH_DATABASE_URL:-postgres://agentmesh:agentmesh@localhost:5432/agentmesh?sslmode=disable}" \
		go run ./cmd/control-plane
```

And add a short-mode test target next to the existing `test`:

```makefile
.PHONY: test-short
test-short: ## Run only the fast tests (skips container-backed tests)
	go test -short -race ./...
```

- [ ] **Step 12: Run the full local stack end to end**

Run each in order:

```bash
make dev
make migrate
make seed
make run-control-plane &
sleep 2
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/readyz
curl -s http://localhost:8080/v1/tenants
curl -s http://localhost:8080/v1/tenants/acme/agents?capability=security-review
kill %1
```

Expected:
- `/healthz` → `{"status":"ok"}`
- `/readyz` → `{"status":"ready"}`
- `/v1/tenants` → a list containing the `acme` tenant
- the capability query → exactly one agent, `security-agent`
- `make seed` run a second time prints "already exists, skipping" for each agent and exits 0

- [ ] **Step 13: Verify a failure path by hand**

Run: `docker compose stop postgres && curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8080/readyz` with the server still running, then `docker compose start postgres`.
Expected: `/readyz` returns `503` while PostgreSQL is stopped and `200` again once it is back, while `/healthz` stays `200` throughout. This is the liveness-versus-readiness distinction behaving as designed.

- [ ] **Step 14: Run build, vet, race, and lint**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./...`
Expected: all four exit 0.

- [ ] **Step 15: Commit**

```bash
git add internal/config/ internal/store/postgres/pinger.go cmd/ Makefile docker-compose.yml .env.example .gitignore
git commit -m "feat: wire control plane to PostgreSQL with compose, seed, and readiness"
```

---
### Task 10: Documentation

**Files:**
- Create: `docs/adr/0008-gorm-for-database-access.md`
- Modify: `docs/learning-guide.md`, `docs/architecture.md`, `README.md`

**Interfaces:**
- Consumes: the shipped behavior of Tasks 1–9. Nothing in this task changes code.
- Produces: no code.

This task is not optional polish. The learning guide is the project's primary teaching document, and the rule stated in its own header is that entries are added when the phase ships — so Phase 1 is not finished until it has one.

- [ ] **Step 1: Write ADR 0008**

Create `docs/adr/0008-gorm-for-database-access.md`:

```markdown
# ADR 0008: GORM for Database Access

Status: Accepted

## Context

Phase 1 introduces PostgreSQL persistence for tenants, agents, and
capabilities. The control plane needs a way to issue queries, map rows to
Go structs, and manage transactions.

Three options were considered: `pgx` with hand-written SQL, `sqlc` with
generated type-safe queries, and GORM as a full ORM.

## Decision

Use GORM (`gorm.io/gorm` with `gorm.io/driver/postgres`), confined entirely
to `internal/store/postgres`.

Schema changes are **not** managed by GORM. Migrations are numbered `.sql`
files embedded with `go:embed` and applied by `internal/migrate`.
`AutoMigrate` is never called.

## Alternatives Considered

- **pgx with hand-written SQL** — every query visible, no code generation,
  the closest fit to the project's stdlib-first preference. Rejected for
  Phase 1 in favour of development speed on what is largely CRUD.
- **sqlc** — real SQL with generated type-safe Go, no runtime ORM. Rejected
  to avoid a second code-generation step alongside `buf`.

## Consequences

GORM writes the SQL, which means the query plan is less visible than it
would be with hand-written statements. Two specific tensions follow, and
both are mitigated rather than ignored:

**Tension with ADR 0002 (Row-Level Security).** ADR 0002 names PostgreSQL
RLS as the tenant-isolation backstop. The usual way to drive RLS is
`SET LOCAL app.tenant_id` inside a transaction, which requires the session
variable and the query to run on the same pooled connection. GORM's
connection pooling makes that awkward.

*Mitigation:* GORM is confined to `internal/store/postgres`. Handlers
depend only on the `store.TenantStore` and `store.AgentStore` interfaces
and never see a `*gorm.DB`. Phase 5 can therefore add a transaction-scoped
RLS wrapper, or replace a store implementation with hand-written SQL,
without touching a line of handler code. This confinement is verified by a
grep step in the Phase 1 implementation plan, not merely by convention.

**Tension with explicit schema control.** `AutoMigrate` cannot reliably
express `ON DELETE CASCADE`, never drops or renames, and leaves no
reviewable record of a change.

*Mitigation:* GORM is used for queries only. The schema belongs to the
`.sql` migration files, and the store tests run against a real PostgreSQL
instance migrated by those files — so a mismatch between the GORM models
and the real schema fails the test suite rather than production.

GORM also brings behavior that has to be managed deliberately:
`SkipDefaultTransaction` is set so writes are not silently wrapped in
transactions, and association auto-save is disabled with `Omit` where
capabilities are inserted explicitly, so that each insert's error can be
translated individually.
```

- [ ] **Step 2: Append the Phase 1 section to the learning guide**

In `docs/learning-guide.md`, replace the trailing section:

```markdown
## Phases 1–12

Not yet written — each phase's section is added here when that phase ships,
following the same structure as the Phase 0 entries above.
```

with the Phase 1 section below, followed by a new `## Phases 2–12` stub carrying the same sentence.

Write entries for these seven topics, each following the exact Phase 0 structure (what problem it solves, why it exists, how it works, files involved, how to test it, common failure modes, how it relates to real distributed systems, alternative approaches, interview questions this might raise):

1. **Ports and adapters: the api → store → domain layering** — why the dependency arrow points one way, what the store interface buys, how the grep check enforces it, and what Phase 5's RLS work depends on.
2. **Multi-tenancy enforced by the schema** — `UNIQUE (tenant_id, name)` rather than application-level checks, why the database is the only component that can settle a race, and the concurrent-create test that proves it.
3. **Explicit SQL migrations over AutoMigrate** — the `schema_migrations` table, applying each migration inside a transaction so "recorded but half-applied" cannot happen, and why `ON DELETE CASCADE` is the specific thing AutoMigrate cannot express.
4. **The transaction boundary lives in the store** — why a handler that knows about transactions needs a `*gorm.DB` and collapses the seam, and what the rollback test actually proves.
5. **Error translation as a membrane** — SQLSTATE `23505` to `domain.ErrAlreadyExists` to HTTP 409, why unknown errors pass through untranslated, and why a 500 never echoes the database error.
6. **Cursor pagination over offset** — the `OFFSET 10000` cost, the skipped-row problem under concurrent writes, the `(created_at, id)` tuple comparison, and the limit+1 trick for detecting a next page.
7. **Liveness versus readiness** — why `/healthz` ignores the database and `/readyz` checks it, and how conflating them produces restart loops.

Each entry's prose is written in Bahasa Indonesia, matching the code comments. Keep the nine English section headings (**What problem it solves:**, **Why it exists:**, and so on) so the structure stays identical to Phase 0's entries, which remain in English.

- [ ] **Step 3: Update the architecture document**

In `docs/architecture.md`, change the status line:

```markdown
**Current status:** Phase 0 complete. Only the control-plane HTTP service
(`cmd/control-plane`, health endpoint only) is implemented. Everything else
below is planned.
```

to:

```markdown
**Current status:** Phase 1 complete. The control plane exposes a REST API
for tenants, agents, and capabilities backed by PostgreSQL
(`/v1/tenants/...`, plus `/healthz` and `/readyz`). The data plane, the
example agents, and configuration distribution are not implemented yet.
```

Add a short "Phase 1 data model" subsection after the component table, giving the three tables and their relationships in prose plus the `UNIQUE (tenant_id, name)` constraint, and note explicitly that **no endpoint is authenticated yet** — Phase 5 adds that.

- [ ] **Step 4: Update the README**

In `README.md`:

- Change "**Phase 0 of 12 complete: repository bootstrap.**" to "**Phase 1 of 12 complete: agent registry and REST API.**"
- Rewrite the "What exists right now" paragraph to list the registry, the REST API, PostgreSQL persistence, migrations, and the seed data.
- Replace the Quick Start block with the working sequence:

```bash
git clone <this-repo>
cd agentmesh
make dev        # start PostgreSQL in Docker
make migrate    # apply the schema
make seed       # load the acme demo tenant and three agents
make run-control-plane

# in another terminal:
curl -s http://localhost:8080/readyz
curl -s http://localhost:8080/v1/tenants
curl -s "http://localhost:8080/v1/tenants/acme/agents?capability=security-review"
```

- Add an "API" section listing the ten `/v1` endpoints and the error envelope shape.
- Under "Security Model", state plainly: **every endpoint is currently unauthenticated.** Authentication and authorization arrive in Phase 5. Do not leave this implied.
- Mark Phase 1 as complete in the roadmap table.

- [ ] **Step 5: Verify every documented command actually works**

Run, in a clean shell, each command the README's Quick Start block now contains, in order.
Expected: every command succeeds and produces the documented output. A README that does not run is worse than no README, and this is the step that catches it.

- [ ] **Step 6: Run the full verification suite one last time**

Run: `go build ./... && go vet ./... && go test -race ./... && golangci-lint run ./... && cd api/proto && buf lint`
Expected: all five exit 0.

- [ ] **Step 7: Confirm the layering constraints still hold**

Run:

```bash
go list -deps ./internal/domain | grep -E 'gorm|net/http' || echo "domain is clean"
grep -rln 'gorm.io' --include='*.go' . | grep -v '^./internal/store/postgres/' || echo "gorm is confined"
grep -rln '"net/http"' --include='*.go' ./internal | grep -v '^./internal/api/' || echo "net/http is confined"
```

Expected: all three print their "clean"/"confined" message. These are the structural invariants the whole design rests on, and they are cheap to check.

- [ ] **Step 8: Commit**

```bash
git add docs/ README.md
git commit -m "docs: add Phase 1 learning guide, ADR 0008, and README updates"
```

- [ ] **Step 9: Tag phase completion**

```bash
git commit --allow-empty -m "chore: complete Phase 1 (agent registry, control plane, REST API)"
```

---

## Verification Summary

After Task 10, all of the following must be true:

| Check | Command |
|---|---|
| Builds | `go build ./...` |
| Vets clean | `go vet ./...` |
| Tests pass with the race detector | `go test -race ./...` |
| Fast tests pass without Docker | `go test -short -race ./...` |
| Lints clean | `golangci-lint run ./...` |
| Protos lint clean | `cd api/proto && buf lint` |
| `domain` imports nothing forbidden | `go list -deps ./internal/domain \| grep -E 'gorm\|net/http'` returns nothing |
| GORM confined to one package | grep from Task 10 Step 7 returns nothing |
| Local stack runs | `make dev && make migrate && make seed && make run-control-plane` |
| Working tree clean | `git status` |
