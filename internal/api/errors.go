package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"agentmesh/internal/domain"
)

// Satu bentuk error untuk seluruh API. Karena bentuknya tunggal, CLI dan
// dashboard cukup menulis satu parser kegagalan, bukan satu per endpoint.
type (
	// errorResponse adalah pembungkus terluar body error.
	errorResponse struct {
		Error errorBody `json:"error"`
	}

	// errorBody memuat kode mesin, pesan untuk manusia, dan detail per field.
	errorBody struct {
		Code    string        `json:"code"`
		Message string        `json:"message"`
		Details []errorDetail `json:"details,omitempty"`
	}

	// errorDetail menunjuk satu field yang bermasalah.
	errorDetail struct {
		Field string `json:"field"`
		Issue string `json:"issue"`
	}
)

// Kode error yang dikenal klien. Ini bagian dari kontrak API, jadi nilainya
// tidak boleh berubah sembarangan; pesan boleh berubah, kode tidak.
const (
	codeInvalid       = "invalid"
	codeNotFound      = "not_found"
	codeAlreadyExists = "already_exists"
	codeInternal      = "internal"
)

// writeError memetakan error domain menjadi response HTTP.
//
// Pemetaan terpusat di sini supaya tidak ada handler yang diam-diam
// memilih status code sendiri. Kalau suatu saat ErrNotFound perlu menjadi
// 410 alih-alih 404, satu tempat ini yang diubah.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := classify(err)

	body := errorResponse{Error: errorBody{Code: code, Message: message}}

	// Detail per field hanya ada pada kegagalan validasi.
	var ve domain.ValidationError
	if errors.As(err, &ve) {
		body.Error.Details = []errorDetail{{Field: ve.Field, Issue: ve.Issue}}
	}

	// Error 500 selalu dicatat lengkap, karena hanya log yang menyimpan
	// penyebab sebenarnya — body response sengaja dibuat generik.
	if status == http.StatusInternalServerError {
		LoggerFrom(r.Context()).Error("request failed",
			"error", err.Error(),
			"method", r.Method,
			"path", r.URL.Path,
		)
	}

	writeJSON(w, status, body)
}

// classify menentukan status, kode, dan pesan dari sebuah error.
//
// Urutan pengecekan penting: ValidationError membungkus ErrInvalid, jadi
// ia harus diperiksa sebelum pengecekan ErrInvalid yang lebih umum agar
// pesannya yang lebih informatif tidak tertimpa.
func classify(err error) (status int, code, message string) {
	var ve domain.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, codeInvalid, ve.Error()
	case errors.Is(err, domain.ErrInvalid):
		return http.StatusBadRequest, codeInvalid, "the request is invalid"
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound, codeNotFound, "the requested resource does not exist"
	case errors.Is(err, domain.ErrAlreadyExists):
		return http.StatusConflict, codeAlreadyExists, "a resource with that identity already exists"
	default:
		// Pesan generik. Mengirim err.Error() apa adanya ke klien akan
		// membocorkan nama constraint, nama tabel, dan potongan DSN —
		// peta skema gratis bagi penyerang.
		return http.StatusInternalServerError, codeInternal, "internal server error"
	}
}

// writeJSON menulis satu nilai sebagai JSON beserta header-nya.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	// Error di sini berarti koneksi sudah putus di tengah penulisan;
	// status sudah terkirim sehingga tidak ada lagi yang bisa dilakukan.
	_ = json.NewEncoder(w).Encode(v)
}
