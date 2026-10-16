package postgres

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// cursorPayload adalah isi cursor sebelum dikodekan. Nama field sengaja
// pendek karena ikut terbawa di setiap URL halaman berikutnya.
type cursorPayload struct {
	T time.Time `json:"t"`
	I string    `json:"i"`
}

// encodeCursor mengemas kunci urut terakhir menjadi satu string buram.
//
// Dipakai paginasi berbasis cursor, bukan OFFSET. Dengan OFFSET, database
// harus membaca lalu membuang baris yang dilewati — OFFSET 10000 membaca
// 10.050 baris untuk mengembalikan 50. Lebih buruk lagi, bila ada baris
// baru masuk di tengah penelusuran, seluruh halaman berikutnya bergeser dan
// ada data yang terlewat tanpa jejak. Cursor membawa kunci urut terakhir,
// sehingga query berikutnya cukup melakukan seek ber-index dan hasilnya
// tetap konsisten walau ada penulisan bersamaan.
//
// Isinya sengaja dibuat buram bagi klien agar urutan bisa diubah kelak
// tanpa memutus siapa pun.
func encodeCursor(createdAt time.Time, id uuid.UUID) string {
	payload := cursorPayload{T: createdAt.UTC(), I: id.String()}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Tidak mungkin terjadi: struct di atas selalu bisa di-marshal.
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor membongkar cursor yang dikirim klien.
//
// Seluruh kegagalan dikembalikan sebagai domain.ErrInvalid, bukan error
// internal, karena cursor rusak adalah kesalahan permintaan (400) dan bukan
// kesalahan server (500).
func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is not valid base64url", domain.ErrInvalid)
	}

	var payload cursorPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is not valid JSON", domain.ErrInvalid)
	}
	if payload.T.IsZero() || payload.I == "" {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor is missing required fields", domain.ErrInvalid)
	}

	id, err := uuid.Parse(payload.I)
	if err != nil {
		return time.Time{}, uuid.Nil,
			fmt.Errorf("%w: cursor contains an invalid id", domain.ErrInvalid)
	}
	return payload.T, id, nil
}

// clampLimit memaksa ukuran halaman berada dalam rentang yang masuk akal.
// Nilai nol atau negatif memakai default; nilai di atas batas dipotong,
// bukan ditolak, agar klien yang meminta terlalu banyak tetap dilayani.
func clampLimit(n int) int {
	switch {
	case n <= 0:
		return store.DefaultLimit
	case n > store.MaxLimit:
		return store.MaxLimit
	default:
		return n
	}
}
