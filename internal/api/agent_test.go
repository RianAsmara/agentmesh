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
