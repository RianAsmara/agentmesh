package api

import (
	"net/http"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// TenantHandler melayani endpoint /v1/tenants.
//
// Field-nya adalah interface store, bukan *gorm.DB. Handler di sini tidak
// punya cara untuk mengetahui apa yang ada di baliknya — itulah sifat yang
// membuat Phase 5 bisa menyisipkan Row-Level Security tanpa menyentuh
// berkas ini.
type TenantHandler struct {
	store store.TenantStore
}

// NewTenantHandler membuat TenantHandler baru.
func NewTenantHandler(s store.TenantStore) *TenantHandler {
	return &TenantHandler{store: s}
}

// Create melayani POST /v1/tenants.
func (h *TenantHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	tenant := domain.Tenant{
		ID:   uuid.New(),
		Slug: req.Slug,
		Name: req.Name,
		// Status tidak diambil dari permintaan. Tenant baru selalu aktif;
		// menonaktifkannya adalah operasi tersendiri lewat PATCH, bukan
		// sesuatu yang bisa disisipkan saat pembuatan.
		Status: domain.TenantActive,
	}

	// Validasi dijalankan sebelum menyentuh store. Dengan begitu input
	// yang jelas salah tidak pernah menghabiskan satu pun koneksi database.
	if err := tenant.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.store.Create(r.Context(), &tenant); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, newTenantResponse(tenant))
}

// Get melayani GET /v1/tenants/{slug}.
func (h *TenantHandler) Get(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	tenant, err := h.store.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTenantResponse(*tenant))
}

// List melayani GET /v1/tenants.
func (h *TenantHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		writeError(w, r, err)
		return
	}

	items, next, err := h.store.List(r.Context(), store.TenantFilter{
		Limit:  limit,
		Cursor: r.URL.Query().Get("cursor"),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}

	// Slice dibuat dengan make, bukan var, supaya daftar kosong terkirim
	// sebagai [] dan bukan null. Klien yang melakukan iterasi atas null
	// akan gagal di banyak bahasa.
	resp := listResponse[tenantResponse]{
		Items:      make([]tenantResponse, 0, len(items)),
		NextCursor: next,
	}
	for _, t := range items {
		resp.Items = append(resp.Items, newTenantResponse(t))
	}

	writeJSON(w, http.StatusOK, resp)
}

// Patch melayani PATCH /v1/tenants/{slug}.
//
// Hanya name dan status yang bisa diubah. Slug tidak: ia identitas di URL,
// dan mengubahnya akan memutus setiap tautan serta setiap baris log yang
// sudah menyebutnya. Karena decodeJSON menolak field asing, percobaan
// mengirim "slug" otomatis berakhir sebagai 400 tanpa penanganan khusus.
func (h *TenantHandler) Patch(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	var req patchTenantRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, r, err)
		return
	}

	tenant, err := h.store.GetBySlug(r.Context(), slug)
	if err != nil {
		writeError(w, r, err)
		return
	}

	// Field berupa pointer: nil berarti klien tidak mengirimnya, sehingga
	// nilai lama dipertahankan. Tanpa pointer, body {} akan mengosongkan
	// seluruh kolom yang bisa diubah.
	if req.Name != nil {
		tenant.Name = *req.Name
	}
	if req.Status != nil {
		tenant.Status = domain.TenantStatus(*req.Status)
	}

	if err := tenant.Validate(); err != nil {
		writeError(w, r, err)
		return
	}

	if err := h.store.Update(r.Context(), tenant); err != nil {
		writeError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, newTenantResponse(*tenant))
}

// Delete melayani DELETE /v1/tenants/{slug}. Agent dan kapabilitas milik
// tenant ikut terhapus lewat ON DELETE CASCADE.
func (h *TenantHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Delete(r.Context(), r.PathValue("slug")); err != nil {
		writeError(w, r, err)
		return
	}

	// 204 No Content: penghapusan berhasil dan memang tidak ada yang perlu
	// dikembalikan.
	w.WriteHeader(http.StatusNoContent)
}
