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
