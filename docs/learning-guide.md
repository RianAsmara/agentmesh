# AgentMesh Learning Guide

This is the primary teaching document for the project. For every major
feature, once implemented, this guide explains: what problem it solves, why
it exists, how it works, which files are involved, how to test it, common
failure modes, how it relates to real distributed systems, alternative
approaches, and likely interview questions.

Entries are added phase by phase, as each feature is actually built — this
guide never describes behavior ahead of the code that implements it.

## Phase 0 — Bootstrap

### Structured JSON Logging

**What problem it solves:** every AgentMesh service needs consistent,
machine-parseable logs (service name, level, message, structured fields)
instead of ad hoc `fmt.Println` output.

**Why it exists:** structured logs are what make the later observability
work (Phase 8) — correlating logs with traces and metrics by request ID —
possible at all.

**How it works:** `internal/observability.NewLogger(w io.Writer, service,
level string) *slog.Logger` wraps the standard library's `log/slog` with a
`slog.NewJSONHandler`, pre-attaches a `service` field via `.With(...)`, and
maps a string level (`"debug"`, `"info"`, `"warn"`, `"error"`) to an
`slog.Level`, defaulting unknown values to `info`.

**Files involved:** `internal/observability/logger.go`,
`internal/observability/logger_test.go`.

**How to test it:** `go test ./internal/observability/... -v`. The tests
write to an in-memory `bytes.Buffer` (not `os.Stdout`) so log level
filtering and field presence can be asserted directly.

**Common failure modes:** passing an invalid level string silently falls
back to `info` rather than erroring — this is intentional (logging setup
should never itself crash a service), but it means a typo'd
`AGENTMESH_LOG_LEVEL` fails silently rather than loudly. Startup validation
in `internal/config` (see below) is what actually catches that typo.

**How it relates to real distributed systems:** production services almost
universally emit structured (usually JSON) logs so a log aggregator
(Loki, Elasticsearch, CloudWatch Logs Insights) can index and query
individual fields instead of grepping free text.

**Alternative approaches:** Zap (higher throughput, more allocation-tuned,
but another dependency and a different API from the stdlib); Logrus (older,
slower, mostly superseded by `slog` since Go 1.21). `slog` was chosen to
follow the project's own preference for standard-library patterns where
practical.

**Interview questions this might raise:** Why JSON logs over plain text?
What's the cost of structured logging vs. `fmt.Println`? How would you add
request-scoped fields (e.g. `trace_id`) to every log line inside a single
request's handler chain without passing the logger through every function
signature? (Hint for later phases: `context.Context`-carried logger, or
`slog.Logger.With` per request.)

### Environment-Based Configuration with Startup Validation

**What problem it solves:** services need runtime configuration (listen
address, log level, timeouts) that varies between local development,
tests, and (eventually) containers/Kubernetes — without a config file
format to parse and without silently running with a nonsensical value.

**Why it exists:** "fails fast at startup with a clear error" is much
cheaper to debug than "started fine, but requests behave strangely because
`AGENTMESH_LOG_LEVEL=verboes` was silently ignored."

**How it works:** `internal/config.Load() (*Config, error)` reads
environment variables (`AGENTMESH_HTTP_ADDR`, `AGENTMESH_LOG_LEVEL`,
`AGENTMESH_SHUTDOWN_TIMEOUT`), falls back to defaults for anything unset,
and calls an internal `validate()` method that rejects unknown log levels,
an empty address, or a non-positive shutdown timeout before `Load` ever
returns successfully.

**Files involved:** `internal/config/config.go`,
`internal/config/config_test.go`.

**How to test it:** `go test ./internal/config/... -v`. Tests use
`t.Setenv` (automatically restores the previous environment after each
test, and marks the test as non-parallel-safe) to exercise defaults,
overrides, and each validation failure path.

**Common failure modes:** forgetting that `t.Setenv` cannot be combined
with `t.Parallel()` in the same test — if a future test in this package
needs to run in parallel, it must not also call `t.Setenv`.

**How it relates to real distributed systems:** this is the same
"twelve-factor app" configuration pattern (config via environment,
validated at process start) used by virtually every container-native
service, because it works identically in a local shell, a Docker Compose
service, and a Kubernetes pod spec's `env:` block.

**Alternative approaches:** a YAML/TOML config file (adds a parsing
dependency and a file-path-resolution problem — where does the file live
in a container?); a flags-based CLI config library like `spf13/viper`
(more features than needed here, and another dependency, contra the
project's stdlib-first preference).

**Interview questions this might raise:** Why validate at startup instead
of at first use? What's the trade-off between environment variables and a
config file for secrets specifically? How would you support hot-reloading
configuration without restarting the process, and what parts of `Config`
would be unsafe to hot-reload (e.g. `HTTPAddr`, which is bound to a
listening socket) versus safe (e.g. `LogLevel`)?

### Graceful Shutdown

**What problem it solves:** a service killed mid-request (e.g. by a
Kubernetes pod eviction or a plain Ctrl-C) should finish in-flight work
rather than dropping connections abruptly.

**Why it exists:** this is a baseline production-readiness requirement
called out explicitly in the project's reliability goals — it matters even
more once the router is forwarding live agent invocations.

**How it works:** `cmd/control-plane/main.go`'s `run` function uses
`signal.NotifyContext` to build a `context.Context` that's cancelled on
`SIGINT`/`SIGTERM`, runs `http.Server.ListenAndServe` in a goroutine
reporting to an error channel, and on either an unexpected server error or
a shutdown signal, calls `http.Server.Shutdown` with a bounded
`context.WithTimeout(cfg.ShutdownTimeout)` so shutdown can't hang forever.

**Files involved:** `cmd/control-plane/main.go`.

**How to test it:** manually — `go run ./cmd/control-plane`, then send
`SIGINT` (Ctrl-C) or `SIGTERM` and confirm the "shutdown signal received"
and "control-plane stopped cleanly" log lines appear and the process exits
0. (No automated test for this yet; Task 4's automated tests cover routing
only. Testing signal-driven shutdown deterministically is worth revisiting
once the router, which has real in-flight-request stakes, is built in
Phase 3.)

**Common failure modes:** a `ShutdownTimeout` set too low truncates
legitimate in-flight requests; set to zero or negative, `config.Load`
rejects it at startup rather than allowing a server that shuts down
instantly.

**How it relates to real distributed systems:** this is exactly what
Kubernetes expects from a well-behaved pod: on `SIGTERM`, stop accepting
new work, finish what's in flight within the grace period, then exit —
otherwise the kubelet sends `SIGKILL` after `terminationGracePeriodSeconds`
and in-flight requests are simply dropped.

**Alternative approaches:** immediately closing all connections on signal
(simpler, but drops in-flight requests — unacceptable once real traffic
flows through this service); a supervisor/process-manager-driven restart
strategy instead of in-process signal handling (pushes the same problem
into infrastructure rather than solving it in the service).

**Interview questions this might raise:** What's the difference between
`http.Server.Close()` and `http.Server.Shutdown(ctx)`? What happens to a
request that's still running when the shutdown timeout expires? How does
this interact with a Kubernetes readiness probe (hint: Phase 7 adds
readiness/liveness endpoints — readiness should flip to "not ready"
*before* shutdown begins, so the load balancer stops sending new traffic
ahead of the process actually stopping).

### Protobuf Tooling (buf)

**What problem it solves:** protobuf/gRPC code generation via raw `protoc`
invocations is fragile (plugin versions, include paths, per-language flags
all have to match by hand across every developer's machine and CI).

**Why it exists:** Phase 2 defines real gRPC services on top of this
tooling; setting up the config now means Phase 2 is "write `.proto`, run
`make generate`," not "first invent a build pipeline."

**How it works:** `api/proto/buf.yaml` declares the module and enables
standard lint/breaking-change rules; `api/proto/buf.gen.yaml` declares two
local plugins (`protoc-gen-go`, `protoc-gen-go-grpc`) generating into
`api/proto/gen/go` with `paths=source_relative`. `make generate` installs
`buf` and both plugins via `go install` if missing, then runs `buf
generate`.

**Files involved:** `api/proto/buf.yaml`, `api/proto/buf.gen.yaml`,
`api/proto/agentmesh/v1/common.proto`, the `generate` target in `Makefile`.

**How to test it:** `make generate`, then confirm
`api/proto/gen/go/agentmesh/v1/common.pb.go` exists and `go build ./...`
still succeeds. This requires network access to the Go module proxy the
first time it runs (to install `buf` and the plugins); see the note in the
Phase 0 implementation plan if that step fails in a sandboxed environment.

**Common failure modes:** editing generated `.pb.go` files by hand — any
such edit is silently discarded the next time `make generate` runs. Proto
field renumbering or removal without following buf's breaking-change rules
(`buf breaking`) can silently break wire compatibility with already-deployed
clients.

**How it relates to real distributed systems:** schema-first API design
with generated, strongly-typed client/server code (protobuf, plus
alternatives like Thrift or Avro) is the standard way large systems keep
service boundaries in sync without hand-written serialization code, and
`buf`'s breaking-change detection is what stops an accidental
backward-incompatible field change from shipping.

**Alternative approaches:** raw `protoc` + shell scripts (more moving parts
to keep in sync across machines and CI, no built-in lint/breaking-change
checks); the Buf Schema Registry's remote plugins instead of locally
installed ones (removes the local `go install` step but adds a runtime
dependency on a third-party registry being reachable at generate time,
which is a worse fit for this project's "runs offline once dependencies are
cached" goal).

**Interview questions this might raise:** Why protobuf over JSON for
internal service-to-service calls? What does `paths=source_relative`
actually control? How does `buf breaking` know a change is
backward-incompatible, and what categories of proto change are always safe
(adding a new optional field) versus never safe (reusing a field number)?

## Phase 1 — Agent Registry and REST API

Catatan penutup atas pertanyaan terbuka dari Phase 0: salah satu interview
question di akhir bagian *Structured JSON Logging* menanyakan bagaimana
menambahkan field per-permintaan ke setiap baris log tanpa mengoper logger
lewat setiap tanda tangan fungsi. Phase 1 menjawabnya:
`LoggerFrom(ctx)` (`internal/api/middleware.go`) mengambil logger yang sudah
dilekati `request_id` dari context, diisi oleh `WithLogging` di awal setiap
permintaan. Pemakaiannya dibahas di topik 1 dan 5 di bawah.

### 1. Ports and Adapters: the api → store → domain Layering

**What problem it solves:** jika handler HTTP memanggil langsung `*gorm.DB`,
logika bisnis dan logika isolasi tenant akan tersebar di antara kode HTTP
dan kode database, dan menguji handler akan selalu membutuhkan PostgreSQL
yang hidup. Phase 1 memisahkan tiga lapisan — domain (aturan bisnis), store
(kontrak penyimpanan), dan api (HTTP) — supaya masing-masing bisa diuji dan
diubah sendiri-sendiri.

**Why it exists:** Phase 5 berencana menambahkan Row-Level Security yang
bergantung pada transaksi tersemat lewat `SET LOCAL app.tenant_id`. Itu
hanya bisa ditambahkan dengan rapi kalau store tetap satu-satunya tempat
yang mengenal `*gorm.DB` — begitu handler ikut mengenalnya, batas itu
runtuh, dan Phase 5 harus menyentuh ulang setiap handler. Lihat
[ADR 0008](adr/0008-gorm-for-database-access.md) untuk penjelasan tensinya.

**How it works:** panah dependensi mengarah satu arah. `internal/domain`
tidak mengimpor apa pun selain stdlib dan `google/uuid` (lihat komentar
paket di `internal/domain/errors.go`); `internal/store/store.go`
mendefinisikan `TenantStore` dan `AgentStore` sebagai interface yang hanya
menyebut tipe domain, tidak menyebut SQL atau GORM; `internal/store/postgres`
mengimplementasikan interface itu memakai GORM; `internal/api` hanya
memegang field bertipe `store.TenantStore` dan `store.AgentStore` (lihat
`RouterConfig` di `internal/api/router.go`), tidak pernah `*gorm.DB`.
Perakitan nyata — menyuntikkan `postgres.NewTenantStore(db)` ke dalam
`api.RouterConfig` — hanya terjadi satu kali, di `cmd/control-plane/main.go`.
Assertion waktu-kompilasi `var _ store.AgentStore = (*AgentStore)(nil)` di
`internal/store/postgres/agent.go` memastikan implementasi tidak diam-diam
melenceng dari kontrak.

**Files involved:** `internal/domain/*.go`, `internal/store/store.go`,
`internal/store/postgres/*.go`, `internal/api/router.go`,
`internal/api/tenant.go`, `internal/api/agent.go`,
`cmd/control-plane/main.go`.

**How to test it:** `go test ./internal/domain/...` jalan tanpa Docker
karena domain tidak menyentuh database sama sekali. Tiga perintah grep
menegakkan arah panahnya secara mekanis: `go list -deps ./internal/domain |
grep -E 'gorm|net/http'` harus kosong, `grep -rln 'gorm.io' ...` harus
hanya menunjuk ke `internal/store/postgres/`, dan `grep -rln '"net/http"'
...` harus hanya menunjuk ke `internal/api/`.

**Common failure modes:** menambahkan import `gorm.io` apa pun di luar
`internal/store/postgres` akan lolos `go build` tapi gagal di grep check —
itulah gunanya check tersebut, bukan sekadar gaya. Begitu juga sebuah
handler yang mulai menerima `*gorm.DB` langsung ("supaya transaksi lebih
mudah") akan terasa seperti jalan pintas yang masuk akal sampai Phase 5
tiba dan harus membongkarnya kembali.

**How it relates to real distributed systems:** ini adalah pola
ports-and-adapters (hexagonal architecture) yang sama dipakai microservice
besar untuk memisahkan logika domain dari detail infrastruktur — repository
pattern di Java/Spring, atau package store yang serupa di banyak layanan Go
produksi. Tujuannya sama: mengganti database, menambah cache, atau
menyisipkan pemeriksaan keamanan tanpa menulis ulang logika bisnis.

**Alternative approaches:** mengoper struct konkret (`*postgres.TenantStore`)
langsung ke handler tanpa interface — lebih sedikit kode, tapi mengunci
setiap test handler pada PostgreSQL sungguhan. Interface `Repository[T]`
generik tunggal untuk semua entitas — mengurangi duplikasi definisi
interface, tapi kehilangan method bernama sesuai domain seperti `GetBySlug`
dan `GetByName` yang membuat kontrak mudah dibaca.

**Interview questions this might raise:** mengapa store didefinisikan
sebagai interface, bukan struct konkret? Di titik mana dalam kode ini tipe
konkret dan interface akhirnya bertemu? Bagaimana Anda akan menambahkan
layer cache di depan `AgentStore` tanpa mengubah satu baris pun di
`internal/api`?

### 2. Multi-Tenancy Enforced by the Schema

**What problem it solves:** dua permintaan `POST
/v1/tenants/{slug}/agents` yang mendaftarkan nama agent sama persis,
bersamaan, pada tenant yang sama, tidak boleh menghasilkan dua baris.
Pengecekan "cari dulu nama ini ada atau tidak, baru insert" di kode
aplikasi punya jendela waktu antara pencarian dan penyisipan — dua goroutine
bisa sama-sama lolos pengecekan sebelum salah satu selesai menulis.

**Why it exists:** Phase 3 nanti akan mengandalkan nama agent sebagai
bagian dari kunci routing; kalau dua baris dengan nama sama bisa lolos
tersimpan, router tidak punya cara menentukan mana yang dimaksud.

**How it works:** migrasi `0001_init.sql` mendefinisikan `CONSTRAINT
agents_tenant_name_key UNIQUE (tenant_id, name)` pada tabel `agents`, dan
`UNIQUE` sederhana pada kolom `slug` tabel `tenants`. `AgentStore.Create`
(`internal/store/postgres/agent.go`) tidak melakukan pengecekan apa pun
sebelum `INSERT` — ia langsung mencoba menyisipkan, lalu `translateErr`
(`internal/store/postgres/errors.go`) menerjemahkan SQLSTATE `23505`
(unique_violation) menjadi `domain.ErrAlreadyExists`, yang pada gilirannya
dipetakan ke HTTP 409 oleh `classify` (`internal/api/errors.go`). Keputusan
"siapa yang menang" dibuat satu-satunya oleh mesin index PostgreSQL saat
kedua transaksi bersaing mengunci baris index yang sama.

**Files involved:** `internal/migrate/migrations/0001_init.sql`,
`internal/store/postgres/agent.go`, `internal/store/postgres/tenant.go`,
`internal/store/postgres/errors.go`.

**How to test it:**
`internal/store/postgres/agent_test.go:TestAgentStore_ConcurrentCreateYieldsExactlyOneWinner`
menjalankan dua goroutine yang mencoba membuat agent dengan nama sama
secara bersamaan lewat `sync.WaitGroup`, lalu memastikan tepat satu yang
berhasil (`err == nil`) dan sisanya gagal dengan `domain.ErrAlreadyExists`
— bukan dua yang berhasil, dan bukan dua yang gagal.

**Common failure modes:** menambahkan pengecekan "`GetByName` dulu, baru
`Create` kalau belum ada" di lapisan API terasa aman saat ditulis dan lolos
semua test yang berjalan satu per satu, tapi gagal persis pada beban
concurrent yang menjadi alasan constraint ini ada.

**How it relates to real distributed systems:** ini adalah alasan mengapa
sistem terdistribusi menyerahkan keputusan "siapa menang" ke satu otoritas
tunggal yang punya kunci atau index — baik itu unique constraint di
database relasional, compare-and-swap di etcd, atau conditional write di
DynamoDB — bukan ke logika aplikasi yang berjalan di banyak proses tanpa
koordinasi.

**Alternative approaches:** distributed lock (misalnya Redis `SETNX` per
nama agent) sebelum insert — menambah dependensi dan titik kegagalan baru
hanya untuk meniru apa yang sudah disediakan database secara gratis.
Application-level mutex dalam satu proses — tidak menolong sama sekali
begitu ada lebih dari satu replica control-plane.

**Interview questions this might raise:** mengapa pengecekan "exists?" di
kode aplikasi tidak cukup untuk mencegah duplikat? Apa yang sebenarnya
terjadi di level index B-tree PostgreSQL saat dua transaksi mencoba
menyisipkan nilai yang sama? Bagaimana constraint ini berperilaku di bawah
isolation level yang lebih rendah dari default PostgreSQL (READ COMMITTED)?

### 3. Explicit SQL Migrations over AutoMigrate

**What problem it solves:** GORM punya `AutoMigrate` yang bisa membuat
tabel otomatis dari struct Go, tapi ia tidak bisa menyatakan `ON DELETE
CASCADE` secara andal, tidak pernah menghapus kolom atau mengganti nama,
dan tidak meninggalkan jejak review atas perubahan skema.

**Why it exists:** tabel `agents` di Phase 1 butuh `ON DELETE CASCADE` ke
`tenants` (dan `capabilities` ke `agents`) supaya menghapus satu tenant
membereskan seluruh data turunannya tanpa baris yatim — itulah justru yang
tidak bisa dijamin `AutoMigrate`.

**How it works:** `internal/migrate/migrate.go` menanamkan berkas `.sql` di
`internal/migrate/migrations/` lewat `go:embed`, menjalankan masing-masing
secara urut berdasarkan nama berkas (`0001_init.sql`, dst.), dan mencatat
setiap versi yang sudah diterapkan ke tabel `schema_migrations` (dibuat
lewat `CREATE TABLE IF NOT EXISTS` sebelum migrasi mana pun dijalankan,
karena tabel pencatat ini sendiri tidak boleh ikut tercatat). `Run()` aman
dipanggil berulang: versi yang sudah ada di `schema_migrations` dilewati.
Yang membuat "tercatat tapi separuh jadi" mustahil adalah `applyOne`: isi
berkas SQL dan `INSERT` ke `schema_migrations` berjalan di dalam satu
`db.BeginTx`/`Commit` yang sama — kalau SQL migrasinya gagal di tengah
jalan, pencatatan versinya ikut batal lewat `tx.Rollback()` yang dipasang
sebagai `defer`.

**Files involved:** `internal/migrate/migrate.go`,
`internal/migrate/migrations/0001_init.sql`, `cmd/migrate/main.go`.

**How to test it:** `go test ./internal/migrate/...`
(`internal/migrate/migrate_test.go`) menjalankan `Run()` terhadap PostgreSQL
sungguhan dan memverifikasi isi `schema_migrations`; `make migrate`
menjalankan binary sungguhan dan mencetak jumlah migrasi yang diterapkan.

**Common failure modes:** mengedit ulang berkas migrasi yang sudah pernah
diterapkan di lingkungan mana pun (termasuk laptop sendiri) tidak akan
terdeteksi — `schema_migrations` hanya menyimpan nama versi, bukan checksum
isinya, jadi perbedaan isi antara berkas lama dan baru bisa lolos tanpa
peringatan. Memberi nomor urut yang sama atau lebih kecil dari migrasi yang
sudah ada juga lolos secara diam-diam, karena `availableVersions` hanya
mengurutkan nama secara leksikografis.

**How it relates to real distributed systems:** pola "migrasi bernomor,
tercatat di tabel sendiri, diterapkan di dalam transaksi" adalah pola yang
sama dipakai alat seperti Flyway, golang-migrate, dan Rails ActiveRecord
Migrations — alasannya selalu sama: beberapa replica Kubernetes yang start
bersamaan tidak boleh menerapkan migrasi yang sama dua kali atau saling
menimpa.

**Alternative approaches:** golang-migrate atau Flyway sebagai alat migrasi
pihak ketiga — fitur lebih lengkap (migrasi turun/down, checksum berkas),
tapi dependensi tambahan untuk kebutuhan yang masih sederhana di Phase 1.
`AutoMigrate` murni — ditolak karena alasan di atas; dicatat di
[ADR 0008](adr/0008-gorm-for-database-access.md).

**Interview questions this might raise:** mengapa pencatatan versi migrasi
harus berada di transaksi yang sama dengan migrasinya sendiri? Apa yang
terjadi kalau dua replica menjalankan `make migrate` pada saat yang hampir
sama? Bagaimana Anda akan menambahkan migrasi "turun" (rollback) ke desain
ini?

### 4. The Transaction Boundary Lives in the Store

**What problem it solves:** membuat satu agent berarti menyisipkan satu
baris `agents` dan nol atau lebih baris `capabilities`. Kalau baris agent
berhasil tapi salah satu baris capability gagal (misalnya karena
duplikat), hasil akhirnya tidak boleh berupa agent yang berdiri sendiri
tanpa kapabilitas yang dimaksud klien.

**Why it exists:** begitu handler HTTP (lapisan api) perlu tahu soal
transaksi untuk menjamin ini, ia membutuhkan `*gorm.DB`, dan sekat
ports-and-adapters pada topik 1 di atas runtuh. Jadi batas transaksi harus
hidup di lapisan store, sedekat mungkin dengan operasi yang benar-benar
butuh sifat "semua atau tidak sama sekali".

**How it works:** `AgentStore.Create` (`internal/store/postgres/agent.go`)
membungkus seluruh pekerjaannya — insert baris `agents` lewat
`tx.Omit("Capabilities").Create(&m)`, lalu insert setiap baris
`capabilities` satu per satu — di dalam satu `db.Transaction(func(tx
*gorm.DB) error { ... })`. `Omit("Capabilities")` mematikan auto-save
asosiasi GORM supaya setiap insert capability bisa diterjemahkan errornya
sendiri-sendiri lewat `translateErr`, bukan digabung jadi satu error
asosiasi yang kabur. Kalau fungsi closure mengembalikan error apa pun,
GORM melakukan rollback penuh atas transaksi tersebut.

**Files involved:** `internal/store/postgres/agent.go`,
`internal/store/postgres/models.go`, `internal/store/postgres/errors.go`.

**How to test it:**
`internal/store/postgres/agent_test.go:TestAgentStore_CreateRollsBackWhenCapabilityInsertFails`
membuat satu agent dengan dua kapabilitas yang isinya sama persis
(melanggar unique index `agent_id`+`name`+`version` pada capability
kedua), memastikan `Create()` mengembalikan error yang membungkus
`domain.ErrAlreadyExists`, lalu — ini bagian intinya — memanggil
`GetByName` sesudahnya dan memastikan hasilnya `domain.ErrNotFound`. Kalau
rollback tidak benar-benar terjadi, baris agent (dan capability pertama)
akan tertinggal di database meski `Create()` melaporkan gagal.

**Common failure modes:** memindahkan logika "insert agent, lalu insert
capabilities" ke dua pemanggilan store terpisah dari handler (misalnya
`agents.Create(...)` lalu `capabilities.CreateMany(...)`) akan terasa
seperti pemisahan tanggung jawab yang wajar, tapi menghilangkan batas
transaksi di antara keduanya — persis masalah yang test rollback ini
dirancang untuk mencegah terulang.

**How it relates to real distributed systems:** ini adalah prinsip "unit of
work" — operasi yang secara logis satu (di sini: "mendaftarkan agent
beserta kapabilitasnya") harus punya satu batas transaksi, ditentukan oleh
siapa yang paling memahami invarian tersebut, bukan oleh siapa yang paling
mudah memanggil database.

**Alternative approaches:** saga / compensating transaction lintas dua
tabel — berguna kalau kedua tulisan harus melewati dua database atau dua
service berbeda, tapi berlebihan di sini karena `agents` dan
`capabilities` berada di PostgreSQL yang sama dan transaksi native sudah
cukup. Mengizinkan agent tanpa kapabilitas tersimpan sementara menolak
sisanya satu per satu — ditolak karena klien mengirim satu permintaan dan
mengharapkan satu hasil yang konsisten dengannya.

**Interview questions this might raise:** mengapa batas transaksi untuk
"buat agent" berada di `AgentStore`, bukan di handler HTTP atau di
`domain.Agent.Validate`? Apa yang dibuktikan
`TestAgentStore_CreateRollsBackWhenCapabilityInsertFails` yang tidak
dibuktikan oleh test yang hanya memeriksa nilai kembalian error? Apa
konsekuensi performa dari membuka satu transaksi per capability yang
disisipkan satu per satu?

### 5. Error Translation as a Membrane

**What problem it solves:** lapisan HTTP perlu tahu "apakah ini 404, 409,
atau 500?" tanpa harus mengenal SQLSTATE PostgreSQL atau tipe error
spesifik driver pgx, dan tanpa pernah membocorkan detail skema database ke
klien lewat pesan error.

**Why it exists:** tanpa membran penerjemah ini, setiap handler harus
mengimpor `github.com/jackc/pgx/v5/pgconn` hanya untuk memeriksa kode
SQLSTATE — itu sama dengan memindahkan pengetahuan tentang PostgreSQL ke
lapisan yang seharusnya tidak pernah mengenalnya (lihat topik 1).

**How it works:** `translateErr` di `internal/store/postgres/errors.go`
memeriksa apakah error yang dikembalikan GORM bisa di-`errors.As` ke
`*pgconn.PgError`; kode `23505` (unique_violation) diterjemahkan ke
`errors.Join(domain.ErrAlreadyExists, err)`, dan `23503`
(foreign_key_violation) ke `errors.Join(domain.ErrNotFound, err)`.
`gorm.ErrRecordNotFound` juga diterjemahkan ke `domain.ErrNotFound`. Error
yang tidak dikenali diteruskan apa adanya — tidak dipaksa menjadi salah
satu sentinel. Di lapisan api, `classify` (`internal/api/errors.go`)
memetakan `domain.ErrNotFound` ke 404, `domain.ErrAlreadyExists` ke 409,
`domain.ValidationError`/`domain.ErrInvalid` ke 400, dan apa pun yang
tidak cocok dengan ketiganya ke 500 dengan pesan generik "internal server
error" — `err.Error()` milik kegagalan sungguhan tidak pernah dikirim ke
klien, hanya dicatat ke log lewat `LoggerFrom(r.Context()).Error(...)`.

**Files involved:** `internal/store/postgres/errors.go`,
`internal/api/errors.go`.

**How to test it:**
`internal/store/postgres/errors_test.go:TestTranslateErr` menguji setiap
cabang pemetaan kode SQLSTATE secara langsung; `internal/api/errors_test.go`
menguji `classify` dan `writeError` untuk memastikan body 500 tidak pernah
memuat `err.Error()` dari error asli.

**Common failure modes:** membiarkan error yang tidak dikenal (misalnya
koneksi yang putus, atau tabel yang memang hilang) diterjemahkan secara
paksa ke salah satu sentinel domain akan membuat kegagalan infrastruktur
yang serius menyamar sebagai 404 yang tenang — bug seperti ini sangat mahal
dilacak karena tidak ada apa pun di log yang menyebut level keparahannya
yang sebenarnya. Itulah sebabnya `translateErr` sengaja meneruskan error
yang tidak dikenal apa adanya alih-alih menebak.

**How it relates to real distributed systems:** pemisahan "error domain"
dari "error infrastruktur" lewat lapisan penerjemah tunggal adalah pola
umum di sistem besar: gRPC punya status code sendiri yang terpisah dari
error library bahasa, dan banyak service mesh menerjemahkan ulang error
upstream supaya klien hilir tidak pernah bergantung pada detail
implementasi lapisan di belakangnya.

**Alternative approaches:** membiarkan setiap handler menangani
`*pgconn.PgError` sendiri-sendiri — duplikasi logika pemetaan di setiap
handler, dan setiap handler baru berisiko lupa menerjemahkan dengan benar.
Middleware generik yang menangkap semua error dan memetakannya berdasarkan
tipe Go tanpa sentinel domain — lebih sedikit kode, tapi kehilangan
kemampuan membedakan "not found" dari "already exists" tanpa bergantung
pada string pesan yang rapuh.

**Interview questions this might raise:** mengapa error yang tidak dikenal
harus diteruskan apa adanya, bukan dipetakan ke sentinel yang "paling
mendekati"? Apa risikonya kalau `classify` mengirim `err.Error()` mentah ke
body response 500? Bagaimana `errors.Join(domain.ErrAlreadyExists, err)`
berbeda dari `fmt.Errorf("%w", err)` dalam hal `errors.Is` dan `errors.As`?

### 6. Cursor Pagination over Offset

**What problem it solves:** paginasi dengan `OFFSET n` membuat database
membaca dan membuang n baris pertama hanya untuk mengembalikan satu halaman
kecil — `OFFSET 10000 LIMIT 50` membaca 10.050 baris untuk mengembalikan
50. Lebih buruk, kalau ada baris baru masuk di antara dua permintaan
halaman, seluruh baris di belakangnya bergeser posisi dan klien bisa
melewati atau melihat dua kali baris yang sama tanpa ada tanda apa pun
bahwa itu terjadi.

**Why it exists:** endpoint List baik untuk tenant maupun agent dirancang
untuk dipanggil berulang saat menjelajahi daftar besar; Phase 3 nanti akan
menjalankan query serupa dari jalur panas router, jadi pola paginasi yang
benar harus ada sejak awal, bukan ditambal belakangan setelah performa
bermasalah.

**How it works:** cursor adalah string buram yang membungkus tuple
`(created_at, id)` milik baris terakhir pada halaman sebelumnya, dikodekan
base64url oleh `encodeCursor` (`internal/store/postgres/cursor.go`) dan
dibongkar oleh `decodeCursor`. Query halaman berikutnya memakai
perbandingan tuple `(created_at, id) > (?, ?)` — bukan dua kondisi `WHERE
created_at >= ? AND id > ?` yang terpisah — supaya PostgreSQL bisa memakai
index komposit `idx_agents_tenant_created` / `idx_tenants_created` secara
langsung lewat seek, dan supaya dua baris dengan `created_at` identik tetap
terurut unik lewat `id` sebagai tie-breaker. Untuk tahu apakah masih ada
halaman berikutnya tanpa query `COUNT(*)` terpisah, `List()` di
`internal/store/postgres/agent.go` dan `tenant.go` selalu meminta
`Limit(limit + 1)`: kalau baris yang kembali benar-benar `limit+1`, baris
terakhir dibuang dari hasil dan dipakai sebagai bahan cursor berikutnya;
kalau kurang dari itu, `next_cursor` dikosongkan.

**Files involved:** `internal/store/postgres/cursor.go`,
`internal/store/postgres/agent.go`, `internal/store/postgres/tenant.go`,
`internal/migrate/migrations/0001_init.sql` (index
`idx_agents_tenant_created`, `idx_tenants_created`), `internal/api/dto.go`
(`listResponse[T]`).

**How to test it:**
`internal/store/postgres/cursor_test.go:TestCursorRoundTrip` dan
`TestDecodeCursor_RejectsMalformedInput` menguji encode/decode secara
terisolasi (cursor datang dari klien, jadi diperlakukan sebagai input tidak
tepercaya); test List di `agent_test.go` dan `tenant_test.go` menguji trik
`limit+1` dengan menyisipkan lebih banyak baris daripada ukuran halaman dan
memeriksa `next_cursor` terisi persis pada halaman yang seharusnya masih
punya lanjutan.

**Common failure modes:** memakai `OFFSET`/`LIMIT` biasa terasa lebih
sederhana untuk ditulis dan untuk mendukung "lompat ke halaman nomor 5",
tapi kedua keuntungan itu hilang begitu tabel terus ditulis bersamaan —
persis situasi registry agent yang sedang berjalan. Cursor yang dikodekan
tanpa validasi ketat (lihat `decodeCursor`) akan membuat cursor rusak dari
klien berakhir sebagai 500 alih-alih 400 yang jelas.

**How it relates to real distributed systems:** keyset/cursor pagination
adalah pola standar di API skala besar — GraphQL Relay cursor connections,
pagination berbasis `after`/`before` di API REST Stripe dan GitHub —
justru karena semuanya menghadapi masalah yang sama: data yang terus
berubah di bawah paginasi yang sedang berjalan.

**Alternative approaches:** `OFFSET`/`LIMIT` murni — lebih sederhana,
mendukung "lompat ke halaman N", tapi mahal dan tidak konsisten di bawah
penulisan bersamaan seperti dijelaskan di atas. Keyset pagination tanpa
enkode cursor (klien mengirim `created_at` dan `id` mentah sebagai
parameter query) — menghindari base64 encode/decode, tapi membuat bentuk
kunci internal (dan urutan pengurutan) menjadi bagian dari kontrak publik
API, sehingga mengubahnya di kemudian hari memutus klien.

**Interview questions this might raise:** mengapa `OFFSET 10000` lambat,
dan mengapa ia juga bisa salah di bawah penulisan bersamaan, bukan cuma
lambat? Mengapa cursor membandingkan tuple `(created_at, id)`, bukan
`created_at` saja? Bagaimana trik "ambil limit+1 baris" menghindari query
`COUNT(*)` terpisah, dan apa trade-off-nya?

### 7. Liveness versus Readiness

**What problem it solves:** orchestrator (Kubernetes atau yang sejenis)
perlu dua pertanyaan berbeda untuk satu proses: "apakah proses ini masih
hidup dan harus dibiarkan jalan?" dan "apakah proses ini siap menerima
traffic sekarang?" Menjawab keduanya dengan satu probe yang sama membuat
kegagalan sementara pada satu dependensi (di sini, PostgreSQL) terlihat
seperti proses yang harus dibunuh dan dijalankan ulang.

**Why it exists:** begitu Phase 1 menambahkan dependensi PostgreSQL,
control-plane untuk pertama kalinya punya kegagalan yang bukan salah
proses itu sendiri — database bisa restart, hilang koneksi sesaat, atau
belum siap saat proses baru dinyalakan.

**How it works:** `handleHealthz` (`internal/api/router.go`) menjawab
tanpa menyentuh apa pun selain konstanta — ia menjawab "proses ini hidup",
titik. `handleReadyz` memeriksa `api.Pinger` (interface kecil dengan satu
method `Ping(ctx) error`, diimplementasikan oleh `postgres.Pinger` di
`internal/store/postgres/pinger.go`) dan mengembalikan 503 kalau `Ping`
gagal atau kalau Pinger-nya nil sama sekali — kasus nil ditangani secara
eksplisit karena `/readyz` dipanggil tanpa autentikasi dan sesering yang
diinginkan orchestrator; satu interface nil akibat kesalahan wiring tidak
boleh ikut panic. `Ping` sendiri membatasi diri dengan `pingTimeout` 2
detik di dalam dirinya sendiri, bukan di pemanggil, supaya tidak ada
pemanggil yang bisa lupa memasang batas waktu dan membuat satu slot pool
menggantung selamanya menunggu database yang tidak pernah menolak maupun
menerima. Penyebab kegagalan dicatat ke log lewat `LoggerFrom`, tidak
pernah dikirim ke body response, karena endpoint ini bisa diakses tanpa
autentikasi.

**Files involved:** `internal/api/router.go`,
`internal/store/postgres/pinger.go`, `internal/api/router_test.go`.

**How to test it:**
`internal/api/router_test.go:TestRouter_HealthzIgnoresDatabase` memastikan
`/healthz` tetap 200 dengan Pinger palsu yang selalu gagal;
`TestRouter_ReadyzReflectsDatabaseHealth` dan
`TestRouter_ReadyzDoesNotPanicOnNilPinger` menguji kedua kasus gagal; secara
manual, `make dev` lalu `docker compose stop postgres` membuat `curl
/readyz` berbalik ke 503 sementara `/healthz` tetap 200, dan `docker
compose start postgres` mengembalikannya ke 200.

**Common failure modes:** konflasi paling umum adalah memakai satu
endpoint `/health` untuk kedua probe Kubernetes (`livenessProbe` dan
`readinessProbe`) — begitu database berkedip sesaat, `livenessProbe` yang
seharusnya hanya menanyakan "proses hidup?" malah ikut gagal, kubelet
membunuh dan menjalankan ulang pod, dan proses baru langsung menghadapi
database yang sama-sama belum siap — persis restart loop yang coba
dihindari.

**How it relates to real distributed systems:** pemisahan
liveness/readiness adalah konvensi standar Kubernetes (`livenessProbe` vs
`readinessProbe`), dan prinsip yang sama berlaku di luar Kubernetes: load
balancer mana pun yang melakukan health check perlu tahu "berhenti
mengirim traffic" itu keputusan yang jauh lebih murah dan lebih sering
benar daripada "bunuh prosesnya".

**Alternative approaches:** satu endpoint `/health` dengan parameter query
(`?check=live` vs `?check=ready`) — menghindari dua handler, tapi membuat
kontrak API lebih rumit tanpa manfaat nyata dibanding dua path terpisah
yang sudah jadi konvensi umum. Readiness yang memeriksa lebih dari satu
dependensi (database, dan nanti mungkin cache) — relevan begitu Phase 7
menambahkan Redis, tapi berlebihan untuk Phase 1 yang baru punya satu
dependensi.

**Interview questions this might raise:** apa perbedaan konsekuensi
operasional antara `/healthz` yang salah mengembalikan 503 dan `/readyz`
yang salah mengembalikan 503? Mengapa `handleReadyz` memeriksa `Pinger ==
nil` secara eksplisit alih-alih membiarkan pemanggilan method pada nil
interface itu panic dan ditangkap `WithRecovery`? Bagaimana Anda akan
memperluas `/readyz` begitu ada dependensi kedua (misalnya Redis di Phase
7) tanpa membuatnya menjadi satu titik kegagalan tunggal untuk semua
dependensi?

## Known Gaps Carried Into Later Phases

Daftar ini bukan bug yang belum ditemukan — setiap butir sudah diketahui
sejak Phase 1 ditulis, dan sengaja dibiarkan karena menutupnya sekarang
akan menunda pengiriman Phase 1 tanpa manfaat yang sepadan. Setiap butir
menyebut fase yang bertanggung jawab menutupnya. README menautkan ke
bagian ini dari Security Model.

- **Endpoint agent tidak diverifikasi sebagai target gRPC (ditutup di
  Phase 3 atau lebih awal).** `domain.Agent.Validate` memeriksa `Endpoint`
  tidak kosong dan maksimal 512 karakter, tapi spesifikasi (Section 1,
  tabel validasi) juga mensyaratkan ia "parse sebagai target gRPC" —
  separuh aturan itu belum diimplementasikan. Phase 3 yang akan men-dial
  nilai-nilai ini, jadi inilah fase terakhir yang boleh membiarkan gap ini
  terbuka; ini kesenjangan kesesuaian spesifikasi yang sungguhan, bukan
  sekadar kerapian kode.
- **Makna `TenantStatus` "suspended" belum didefinisikan (ditutup di
  Phase 5).** `TenantStatus` didefinisikan `active | suspended` dengan
  default database `active`, tapi spesifikasi tidak mendefinisikan
  semantik perilaku apa pun untuk `suspended` — tidak ada endpoint yang
  menolaknya, tidak ada yang menegakkannya. Phase 5 membangun otorisasi di
  atas field ini, jadi artinya harus diputuskan sebelum itu.
- **Daftar kapabilitas tidak punya batas panjang (ditutup saat store ini
  disentuh lagi, idealnya tidak lebih lambat dari Phase 5/6).**
  Satu-satunya batas hari ini adalah ukuran body permintaan 1 MiB
  (`maxBodyBytes` di `internal/api/dto.go`), dan `AgentStore.Create`
  menyisipkan baris kapabilitas satu per satu di dalam satu transaksi
  (lihat topik 4 di atas), sehingga daftar yang sangat panjang berarti
  transaksi tertahan lama. Pantas diberi batas eksplisit plus insert
  berkelompok saat store ini disentuh lagi.
- **`postgres.Open` tidak menutup `*sql.DB` kalau ping-nya gagal (tidak
  berbahaya hari ini; ditutup begitu `Open` dipanggil dari retry loop).**
  Setiap pemanggil `Open` hari ini langsung keluar begitu ia gagal
  (`cmd/control-plane/main.go`, `cmd/seed/main.go`), jadi koneksi yang
  tidak ditutup ikut hilang bersama proses. Kebocoran sungguhan baru
  muncul kalau suatu saat `Open` dipanggil berulang di dalam retry loop.
- **`statusRecorder` tidak meneruskan `http.Flusher`, `http.Hijacker`,
  atau `io.ReaderFrom` (ditutup begitu ada endpoint streaming/SSE,
  kemungkinan Phase 4 atau lebih lambat).** Tidak ada endpoint Phase 1
  yang membutuhkan interface tersebut, tapi endpoint SSE/streaming mana
  pun di masa depan akan butuh method penerus tambahan di `statusRecorder`
  (`internal/api/middleware.go`) atau harus melewati middleware ini sama
  sekali. Kegagalannya diam-diam: `Flush()` yang dipanggil pada
  `statusRecorder` yang tidak meneruskannya menjadi no-op, bukan error.
- **`PATCH` dengan body `{}` tetap menjalankan `UPDATE` (keputusan yang
  sudah diambil, dicatat di sini supaya tidak dianggap bug).** Karena
  kedua field di `patchTenantRequest`/`patchAgentRequest` bertipe pointer
  dan keduanya nil kalau body-nya `{}`, `Patch` tetap memanggil
  `store.Update`, yang menggerakkan `updated_at` meski tidak ada kolom
  semantik yang benar-benar berubah.
- **Parameter `?capability=` berulang hanya mengambil nilai pertama
  (perilaku `url.Values.Get`; tidak didefinisikan spesifikasi baik atau
  buruk).** `?capability=a&capability=b` memakai nilai `"a"` saja; ini
  bukan bug karena spesifikasi tidak mendefinisikan perilaku untuk filter
  berulang, tapi layak diketahui sebelum router Phase 3 mewarisi query
  yang sama.
- **Go 1.22+ `ServeMux` mencocokkan terhadap path yang sudah di-escape
  (tidak dieksploitasi hari ini; perlu diperhatikan kalau aturan karakter
  slug/name berubah).** `{slug}` dan `{name}` secara teknis bisa menangkap
  nilai yang berisi `%2F` yang sudah ter-decode menjadi `/`. Hari ini aman
  karena `validateSlug` menolak `/` sejak pembuatan dan setiap lookup
  bersifat literal, tapi ini satu hal yang harus diingat kalau aturan
  karakter itu pernah dilonggarkan.
- **TLS ke database tidak ditegakkan (ditutup oleh fase yang pertama kali
  mengirim deployment sungguhan).** Stack lokal memakai `sslmode=disable`
  karena Postgres di `docker-compose.yml` tidak menyajikan TLS. Menegakkan
  TLS adalah urusan fase yang pertama kali mengirim deployment sungguhan,
  bukan urusan stack pengembangan lokal.

## Phases 2–12

Not yet written — each phase's section is added here when that phase ships,
following the same structure as the Phase 0 entries above.
