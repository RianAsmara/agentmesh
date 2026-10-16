package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"agentmesh/internal/domain"
)

// maxBodyBytes membatasi ukuran body permintaan. Tanpa batas ini, satu
// klien bisa membuat server kehabisan memori hanya dengan mengirim JSON
// yang sangat panjang.
const maxBodyBytes = 1 << 20 // 1 MiB

// DTO di bawah ini terpisah dari struct domain dengan sengaja. Nama field
// JSON adalah kontrak publik; nama field domain adalah urusan internal.
// Memisahkannya berarti keduanya bisa berubah sendiri-sendiri.

// tenantResponse adalah bentuk tenant yang dikirim ke klien.
type tenantResponse struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// createTenantRequest adalah body untuk POST /v1/tenants.
type createTenantRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// patchTenantRequest adalah body untuk PATCH /v1/tenants/{slug}.
//
// Field-nya pointer, bukan nilai biasa. Itu yang membedakan "field tidak
// dikirim" dari "field dikirim bernilai kosong": tanpa pointer, body {}
// akan tampak sama dengan {"name":""} dan diam-diam mengosongkan kolom.
type patchTenantRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
}

// listResponse membungkus satu halaman hasil beserta cursor berikutnya.
// Generic supaya bentuk response daftar seragam untuk semua resource.
type listResponse[T any] struct {
	Items []T `json:"items"`
	// NextCursor kosong berarti tidak ada halaman lagi.
	NextCursor string `json:"next_cursor,omitempty"`
}

// newTenantResponse memetakan entitas domain ke bentuk response.
func newTenantResponse(t domain.Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID.String(),
		Slug:      t.Slug,
		Name:      t.Name,
		Status:    string(t.Status),
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
}

// decodeJSON membaca body permintaan menjadi dst.
//
// Semua kegagalan dikembalikan sebagai domain.ErrInvalid supaya berakhir
// sebagai 400, bukan 500: body yang rusak adalah kesalahan klien.
//
// DisallowUnknownFields dinyalakan agar salah ketik nama field — misalnya
// "nmae" — ditolak dengan jelas, bukan diterima lalu diam-diam diabaikan
// sehingga klien mengira permintaannya berhasil.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return fmt.Errorf("%w: request body exceeds 1 MiB", domain.ErrInvalid)
		case errors.Is(err, io.EOF):
			return fmt.Errorf("%w: request body is empty", domain.ErrInvalid)
		default:
			// Detail dari encoding/json memuat nama tipe dan field Go
			// (misalnya `json: unknown field "x"`, atau "...Go struct field
			// createTenantRequest.slug of type string") — itu bentuk internal
			// struct kami. classify() saat ini sudah membuang message untuk
			// cabang ErrInvalid, jadi detail itu tidak sampai ke klien hari
			// ini; menahannya di sini membuat sifat tersebut tidak bergantung
			// pada classify() tetap begitu. Detailnya dikirim ke log level
			// debug supaya operator tidak kehilangan satu-satunya petunjuk
			// mengapa sebuah body ditolak.
			LoggerFrom(r.Context()).Debug("json decode failed",
				"error", err.Error(),
				"path", r.URL.Path,
			)
			return fmt.Errorf("%w: request body is not valid JSON", domain.ErrInvalid)
		}
	}

	// Body harus memuat tepat satu dokumen JSON. Sisa data setelahnya
	// biasanya berarti klien mengirim dua objek berturut-turut.
	if dec.More() {
		return fmt.Errorf("%w: request body must contain exactly one JSON object", domain.ErrInvalid)
	}
	return nil
}

// parseLimit membaca parameter ?limit= dari query string.
//
// Kosong berarti pakai default di lapisan store. Nilai yang bukan angka
// atau negatif ditolak dengan 400, tidak diam-diam diabaikan: klien yang
// salah ketik ?limit=abc lebih baik tahu sekarang daripada bingung kenapa
// jumlah hasilnya tidak sesuai harapan.
func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%w: limit must be a number", domain.ErrInvalid)
	}
	if n < 0 {
		return 0, fmt.Errorf("%w: limit must not be negative", domain.ErrInvalid)
	}
	// Nilai di atas batas tidak ditolak; clampLimit di lapisan store yang
	// memotongnya. Permintaan yang terlalu rakus tetap dilayani, hanya saja
	// hasilnya dibatasi.
	return n, nil
}

// capabilityResponse adalah bentuk kapabilitas yang dikirim ke klien.
type capabilityResponse struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// agentResponse adalah bentuk agent yang dikirim ke klien.
type agentResponse struct {
	ID           string               `json:"id"`
	TenantID     string               `json:"tenant_id"`
	Name         string               `json:"name"`
	Endpoint     string               `json:"endpoint"`
	Status       string               `json:"status"`
	Capabilities []capabilityResponse `json:"capabilities"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
}

// capabilityRequest adalah satu elemen kapabilitas dalam permintaan create.
type capabilityRequest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// createAgentRequest adalah body untuk POST /v1/tenants/{slug}/agents.
//
// Status tidak ada di sini dengan sengaja: agent yang baru didaftarkan
// selalu berstatus unknown karena control plane belum pernah mendengar
// kabar apa pun darinya. Heartbeat yang mengubah status baru ada di Phase 2.
type createAgentRequest struct {
	Name         string              `json:"name"`
	Endpoint     string              `json:"endpoint"`
	Capabilities []capabilityRequest `json:"capabilities"`
}

// patchAgentRequest adalah body untuk PATCH pada satu agent. Pointer
// membedakan "tidak dikirim" dari "dikirim kosong".
type patchAgentRequest struct {
	Endpoint *string `json:"endpoint"`
	Status   *string `json:"status"`
}

// newAgentResponse memetakan entitas domain ke bentuk response.
func newAgentResponse(a domain.Agent) agentResponse {
	caps := make([]capabilityResponse, 0, len(a.Capabilities))
	for _, c := range a.Capabilities {
		caps = append(caps, capabilityResponse{Name: c.Name, Version: c.Version})
	}
	return agentResponse{
		ID:           a.ID.String(),
		TenantID:     a.TenantID.String(),
		Name:         a.Name,
		Endpoint:     a.Endpoint,
		Status:       string(a.Status),
		Capabilities: caps,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}
