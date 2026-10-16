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

	"agentmesh/internal/domain"
	"agentmesh/internal/observability"
	"agentmesh/internal/store"
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

// Review Focus #3: un RouterConfig yang lupa mengisi DB tidak boleh membuat
// /readyz panic. Ini bukan kasus di brief task-8, tapi NewRouter menerima
// Pinger lewat interface — dan memanggil method pada interface nil adalah
// panic run-time di Go, bukan nil-pointer biasa yang bisa dicek dalam
// handler. Mengujinya di sini mengunci perilaku defensif di handleReadyz.
func TestRouter_ReadyzDoesNotPanicOnNilPinger(t *testing.T) {
	router := NewRouter(RouterConfig{
		Logger:  observability.NewLogger(&bytes.Buffer{}, "test", "error"),
		Tenants: newFakeTenantStore(),
		Agents:  newFakeAgentStore(),
		DB:      nil,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d for a nil Pinger: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
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

// panickingTenantStore memaksa panic dari dalam handler, supaya panic itu
// menempuh rangkaian middleware yang dibangun NewRouter — bukan rangkaian
// yang disusun sendiri oleh test.
type panickingTenantStore struct {
	store.TenantStore
}

func (panickingTenantStore) List(context.Context, store.TenantFilter) ([]domain.Tenant, string, error) {
	panic("handler exploded inside the real router")
}

// Urutan middleware di router.go sebelumnya tidak dijaga oleh test apa pun.
// TestWithRecovery_InnermostLogsRequestIDAndSummary menyusun Chain-nya
// sendiri di middleware_test.go, jadi ia membuktikan middleware-nya bisa
// dikomposisi dengan benar — bukan bahwa NewRouter memang mengomposisinya
// begitu. Membalik urutan Chain di router.go ke bentuk cacat (recovery
// paling luar) lolos seluruh suite tanpa satu pun test merah. Test ini
// menutup celah itu: ia menembakkan panic lewat router sungguhan dan
// menuntut dua sifat yang hanya berlaku bila recovery paling dalam —
// panic tercatat DENGAN request id, dan permintaan yang panic tetap
// mendapat baris ringkasan dari WithLogging.
func TestRouter_PanicIsRecoveredWithRequestIDAndSummary(t *testing.T) {
	var logBuf bytes.Buffer

	router := NewRouter(RouterConfig{
		Logger:  observability.NewLogger(&logBuf, "test", "info"),
		Tenants: panickingTenantStore{TenantStore: newFakeTenantStore()},
		Agents:  newFakeAgentStore(),
		DB:      fakePinger{},
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants", nil)
	req.Header.Set(RequestIDHeader, "req-router-panic")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "exploded") {
		t.Errorf("response leaked the panic message: %s", rec.Body.String())
	}

	lines := strings.Split(strings.TrimSpace(logBuf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log produced %d lines, want 2 (panic + request summary); "+
			"a missing summary means recovery is wired outside WithLogging:\n%s",
			len(lines), logBuf.String())
	}

	for i, line := range lines {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %d is not valid JSON: %v\n%s", i, err, line)
		}
		if got := entry["request_id"]; got != "req-router-panic" {
			t.Errorf("log line %d request_id = %v, want %q; "+
				"an empty id means recovery is wired outside WithRequestID",
				i, got, "req-router-panic")
		}
	}
}
