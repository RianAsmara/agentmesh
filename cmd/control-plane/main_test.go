// cmd/control-plane/main_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewMux_HealthzRouting(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
	}{
		{"GET /healthz succeeds", http.MethodGet, "/healthz", http.StatusOK, `{"status":"ok"}`},
		{"POST /healthz not allowed", http.MethodPost, "/healthz", http.StatusMethodNotAllowed, ""},
		{"unknown path returns 404", http.MethodGet, "/nope", http.StatusNotFound, ""},
	}

	mux := newMux()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
		})
	}
}
