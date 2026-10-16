// Package postgres adalah implementasi store berbasis PostgreSQL memakai
// GORM.
//
// Ini satu-satunya package di repository yang boleh mengimpor gorm.io/...
// atau driver pgx. Pembatasan itu yang menjaga agar keputusan memakai GORM
// tetap bisa dibatalkan: ADR 0002 merencanakan Row-Level Security sebagai
// lapisan isolasi tenant, dan pooling koneksi GORM membuat SET LOCAL
// app.tenant_id merepotkan. Karena GORM terkurung di sini, Phase 5 bisa
// membungkus atau mengganti isi package ini saja.
package postgres

import (
	"context"
	"fmt"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Options mengatur pool koneksi database.
type Options struct {
	// MaxOpenConns membatasi total koneksi ke PostgreSQL. Nilai ini harus
	// dijaga: PostgreSQL memakai satu proses per koneksi, jadi batas yang
	// kebesaran justru membebani server, bukan mempercepatnya.
	MaxOpenConns int
	// MaxIdleConns menahan sejumlah koneksi tetap terbuka agar permintaan
	// berikutnya tidak membayar biaya handshake.
	MaxIdleConns int
	// ConnMaxLifetime memaksa koneksi lama didaur ulang, sehingga perubahan
	// DNS atau failover database tidak membuat proses memegang koneksi mati
	// selamanya.
	ConnMaxLifetime time.Duration
}

// Open membuka koneksi GORM ke PostgreSQL dan memverifikasinya dengan ping.
func Open(ctx context.Context, dsn string, opts Options) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		// Logger GORM dimatikan: logging permintaan sudah ditangani
		// middleware di lapisan api memakai slog, dan dua sumber log yang
		// berbeda format justru menyulitkan korelasi.
		Logger: logger.Default.LogMode(logger.Silent),
		// Penamaan kolom dan tabel ditentukan oleh tag di models.go dan oleh
		// berkas migrasi, bukan oleh konvensi GORM.
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access underlying sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(opts.MaxOpenConns)
	sqlDB.SetMaxIdleConns(opts.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(opts.ConnMaxLifetime)

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
