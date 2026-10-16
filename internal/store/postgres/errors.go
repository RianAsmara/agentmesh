package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
)

// Kode SQLSTATE PostgreSQL yang diterjemahkan ke sentinel domain.
// Daftar lengkapnya ada di dokumentasi PostgreSQL, bagian "Error Codes".
const (
	// pgUniqueViolation terjadi saat INSERT atau UPDATE menabrak UNIQUE index.
	pgUniqueViolation = "23505"
	// pgForeignKeyViolation terjadi saat baris menunjuk induk yang tidak ada,
	// misalnya membuat agent untuk tenant_id yang sudah terhapus.
	pgForeignKeyViolation = "23503"
)

// translateErr mengubah kosakata database menjadi kosakata domain.
//
// Fungsi kecil inilah satu-satunya selaput antara keduanya. Di atasnya,
// handler hanya mengenal domain.ErrAlreadyExists dan domain.ErrNotFound,
// dan tidak pernah tahu bahwa PostgreSQL ada.
//
// Error yang tidak dikenali diteruskan apa adanya, bukan dipaksa menjadi
// salah satu sentinel. Memaksakan terjemahan akan membuat kegagalan nyata —
// koneksi putus, tabel hilang — menyamar sebagai 404 yang tenang, dan bug
// seperti itu sangat mahal untuk dilacak.
func translateErr(err error) error {
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case pgUniqueViolation:
			return errors.Join(domain.ErrAlreadyExists, err)
		case pgForeignKeyViolation:
			return errors.Join(domain.ErrNotFound, err)
		}
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.Join(domain.ErrNotFound, err)
	}

	return err
}
