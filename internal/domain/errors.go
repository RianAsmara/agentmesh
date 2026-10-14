// Package domain berisi entitas inti AgentMesh beserta aturan validasinya.
//
// Package ini sengaja tidak mengimpor apa pun selain standard library dan
// google/uuid. Tidak ada GORM, tidak ada net/http. Dengan begitu aturan
// bisnis bisa diuji tanpa database dan tanpa HTTP server.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel error yang dipakai lintas lapisan. Lapisan store menerjemahkan
// error database menjadi salah satu dari error ini, lalu lapisan api
// memetakannya ke status code HTTP. Jadi store tidak perlu tahu soal HTTP,
// dan api tidak perlu tahu soal PostgreSQL.
var (
	// ErrNotFound berarti resource yang diminta tidak ada.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists berarti resource dengan identitas yang sama sudah ada.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalid berarti input tidak memenuhi aturan validasi domain.
	ErrInvalid = errors.New("invalid")
)

// ValidationError menjelaskan field mana yang gagal validasi dan kenapa.
// Error ini membungkus ErrInvalid sehingga pemanggil bisa memakai
// errors.Is(err, ErrInvalid) untuk pengecekan kasar, atau errors.As untuk
// mengambil detail per field saat menyusun response body.
type ValidationError struct {
	Field string
	Issue string
}

// Error memenuhi interface error.
func (e ValidationError) Error() string {
	return fmt.Sprintf("field %q is invalid: %s", e.Field, e.Issue)
}

// Unwrap membuat errors.Is(err, ErrInvalid) bernilai true.
func (e ValidationError) Unwrap() error { return ErrInvalid }

// invalidField adalah helper ringkas untuk membuat ValidationError.
func invalidField(field, issue string) error {
	return ValidationError{Field: field, Issue: issue}
}
