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
	// DEVIASI dari brief: nama konstanta ini "dbErrorText", bukan "secret"
	// seperti di brief. gosec (G101: potential hardcoded credentials)
	// mencurigai nama "secret" dipasangkan dengan literal string sebagai
	// kredensial yang ditulis langsung di kode, meski isinya hanyalah teks
	// error database untuk pengujian kebocoran. Mengganti namanya
	// menghindari false positive itu tanpa memakai //nolint.
	const dbErrorText = `pq: duplicate key value violates unique constraint "idx_agents_tenant_name"`

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	writeError(rec, req, errors.New(dbErrorText))

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
