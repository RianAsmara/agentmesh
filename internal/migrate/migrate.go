// Package migrate menjalankan migrasi skema PostgreSQL dari berkas .sql
// yang ditanam ke dalam binary.
//
// Sengaja tidak memakai AutoMigrate milik GORM. AutoMigrate tidak bisa
// menyatakan ON DELETE CASCADE secara andal, tidak pernah menghapus atau
// mengganti nama kolom, dan tidak meninggalkan catatan perubahan yang bisa
// direview. Berkas SQL bernomor meninggalkan jejak yang jelas.
package migrate

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// createMigrationsTable disiapkan terpisah dan dijalankan sebelum migrasi
// mana pun, karena tabel pencatat ini sendiri tidak boleh ikut tercatat.
const createMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT        PRIMARY KEY,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
)`

// Run menerapkan seluruh migrasi yang belum pernah dijalankan, berurutan
// menurut nama berkas. Aman dipanggil berkali-kali: migrasi yang sudah
// tercatat akan dilewati, sehingga setiap pod yang restart boleh memanggil
// fungsi ini tanpa syarat.
func Run(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, createMigrationsTable); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	applied, err := appliedSet(ctx, db)
	if err != nil {
		return err
	}

	versions, err := availableVersions()
	if err != nil {
		return err
	}

	for _, version := range versions {
		if _, done := applied[version]; done {
			continue
		}
		if err := applyOne(ctx, db, version); err != nil {
			return err
		}
	}
	return nil
}

// Applied mengembalikan daftar versi yang sudah diterapkan, terurut.
func Applied(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return versions, nil
}

// applyOne menjalankan satu berkas migrasi dan mencatatnya, keduanya di
// dalam satu transaksi. Kalau SQL-nya gagal di tengah jalan, catatan versi
// ikut dibatalkan — jadi tidak pernah ada keadaan "tercatat tapi separuh
// jadi", yang merupakan cara paling umum sebuah sistem migrasi rusak.
func applyOne(ctx context.Context, db *sql.DB, version string) error {
	body, err := migrationFS.ReadFile("migrations/" + version + ".sql")
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }() // no-op setelah Commit berhasil

	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}

// appliedSet membaca versi yang sudah diterapkan sebagai set, untuk
// pengecekan cepat di dalam loop.
func appliedSet(ctx context.Context, db *sql.DB) (map[string]struct{}, error) {
	versions, err := Applied(ctx, db)
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(versions))
	for _, v := range versions {
		set[v] = struct{}{}
	}
	return set, nil
}

// availableVersions mendaftar migrasi yang tertanam, terurut menurut nama.
// Penomoran berkas (0001_, 0002_, ...) itulah yang menentukan urutan, jadi
// nomor baru harus selalu lebih besar dari yang sudah ada.
func availableVersions() ([]string, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}

	var versions []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		versions = append(versions, strings.TrimSuffix(e.Name(), ".sql"))
	}
	sort.Strings(versions)
	return versions, nil
}
