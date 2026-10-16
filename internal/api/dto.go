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
			return fmt.Errorf("%w: request body is not valid JSON: %s", domain.ErrInvalid, err.Error())
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
