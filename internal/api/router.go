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

	// Urutan middleware penting, dan recovery dipasang paling DALAM
	// (terakhir di daftar), bukan paling luar.
	//
	// Chain membuat argumen pertama paling luar. WithRecovery membaca
	// request id lewat RequestIDFrom dari context permintaan yang sampai
	// padanya sendiri — bukan dari tembusan yang diteruskan WithRequestID
	// ke lapisan di bawahnya. Kalau recovery dipasang di luar WithRequestID,
	// context yang ia pegang saat deferred handler berjalan belum pernah
	// disentuh WithRequestID, sehingga setiap baris log panic membawa
	// request_id kosong.
	//
	// Ada alasan kedua yang lebih mahal: kalau recovery dipasang di luar
	// WithLogging, panic yang menjalar ke atas melewati frame WithLogging
	// sebelum sempat dipulihkan — permintaan yang crash tidak pernah
	// mendapat baris ringkasannya sama sekali, bukan cuma kehilangan
	// request_id-nya.
	//
	// Memasang recovery paling dalam membuat kedua masalah itu hilang:
	// request id sudah ada di context saat panic dicatat, dan WithLogging
	// tetap membungkus pemulihannya sehingga permintaan yang panic pun
	// mendapat baris ringkasan seperti permintaan lain. Lihat
	// TestWithRecovery_InnermostLogsRequestIDAndSummary di middleware_test.go.
	return Chain(mux,
		WithRequestID(),
		WithLogging(cfg.Logger),
		WithRecovery(cfg.Logger),
	)
}

// handleHealthz adalah probe liveness.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleReadyz adalah probe readiness: ia memeriksa dependensi.
//
// db bisa nil kalau pemanggil NewRouter lupa mengisi RouterConfig.DB — itu
// kesalahan wiring, bukan kesalahan permintaan, tapi endpoint ini tidak
// boleh ikut panic karenanya: /readyz adalah probe yang dipanggil terus
// menerus oleh orchestrator, dan satu nil interface tidak boleh berubah
// menjadi crash loop. db nil diperlakukan sama seperti Ping yang gagal.
func handleReadyz(db Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if db == nil {
			LoggerFrom(r.Context()).Error("readiness check failed", "error", "no database configured")

			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
				"reason": "database is not reachable",
			})
			return
		}

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
