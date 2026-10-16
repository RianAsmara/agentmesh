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
