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
	"time"

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
	// DEVIASI dari brief: brief menuliskan Create tanpa mengisi timestamp,
	// tetapi store.TenantStore sungguhan (lihat postgres/tenant.go:37-40)
	// mengisi CreatedAt/UpdatedAt lewat pointer t sebelum menyimpan, dan
	// TestTenantHandler_Create mengasumsikan kontrak itu (memeriksa
	// CreatedAt tidak nol). Tanpa baris ini test gagal karena fake tidak
	// meniru kontrak store yang sesungguhnya.
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
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
