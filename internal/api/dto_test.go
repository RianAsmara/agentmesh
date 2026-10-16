package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pesan error dari encoding/json memuat nama tipe dan field Go — misalnya
// `json: unknown field "foo"` atau "cannot unmarshal string into Go struct
// field createTenantRequest.slug of type int". Itu bentuk internal struct
// kami, dan tidak boleh ikut ke body response 400. Detailnya tetap bisa
// dibaca di log.
func TestDecodeJSON_DoesNotLeakDecoderDetail(t *testing.T) {
	tests := []struct {
		name string
		body string
		// Potongan yang tidak boleh muncul di body response.
		forbidden []string
	}{
		{
			name:      "unknown field names the field",
			body:      `{"nonexistent_field":"x"}`,
			forbidden: []string{"nonexistent_field", "json:"},
		},
		{
			name:      "type mismatch names the Go struct and type",
			body:      `{"slug":123}`,
			forbidden: []string{"createTenantRequest", "Go struct field", "of type"},
		},
		{
			name:      "syntax error names the offset",
			body:      `{"slug":`,
			forbidden: []string{"unexpected end of JSON", "offset"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/tenants", strings.NewReader(tt.body))

			var dst createTenantRequest
			err := decodeJSON(rec, req, &dst)
			if err == nil {
				t.Fatalf("decodeJSON(%s) = nil, want an error", tt.body)
			}

			// Error ini yang dirender writeError ke body klien.
			writeError(rec, req, err)

			body := rec.Body.String()
			for _, f := range tt.forbidden {
				if strings.Contains(body, f) {
					t.Errorf("400 response leaked decoder detail %q: %s", f, body)
				}
			}
		})
	}
}
