package postgres

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"agentmesh/internal/domain"
	"agentmesh/internal/migrate"
)

// Satu container dipakai bersama oleh seluruh test di package ini.
// Menyalakan PostgreSQL memakan waktu beberapa detik, dan mengulanginya
// untuk setiap test membuat suite terlalu lambat untuk dijalankan sering.
// Isolasi antar-test tetap terjaga karena setiap test bekerja di tenant
// miliknya sendiri; helper newTenant ditambahkan di Task 4, bersama
// TenantStore yang dipakainya.
var (
	sharedOnce sync.Once
	sharedDB   *gorm.DB
	sharedErr  error
)

// testDB mengembalikan koneksi GORM ke PostgreSQL yang skemanya sudah
// dimigrasi.
func testDB(t *testing.T) *gorm.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

	sharedOnce.Do(func() {
		sharedDB, sharedErr = startSharedPostgres()
	})
	if sharedErr != nil {
		t.Fatalf("start shared postgres: %v", sharedErr)
	}
	return sharedDB
}

// startSharedPostgres menyalakan container, menjalankan migrasi, lalu
// mengembalikan koneksi GORM.
//
// Container sengaja tidak dihentikan lewat t.Cleanup: ia dipakai bersama,
// jadi test mana pun yang selesai duluan tidak boleh mematikannya. Ryuk,
// container pendamping milik testcontainers, yang akan membersihkannya
// ketika proses test berakhir.
func startSharedPostgres() (*gorm.DB, error) {
	ctx := context.Background()

	container, err := tcpostgres.Run(ctx,
		"postgres:17-alpine",
		tcpostgres.WithDatabase("agentmesh_test"),
		tcpostgres.WithUsername("agentmesh"),
		tcpostgres.WithPassword("agentmesh"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, err
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}

	// Migrasi dijalankan lewat database/sql, bukan GORM, persis seperti di
	// produksi. Dengan begitu test ini juga membuktikan bahwa model GORM
	// cocok dengan skema yang dihasilkan berkas migrasi — bukan dengan
	// skema yang dikarang GORM sendiri.
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sqlDB.Close() }()

	if err := migrate.Run(ctx, sqlDB); err != nil {
		return nil, err
	}

	return Open(ctx, dsn, Options{
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	})
}

// newTenant membuat satu tenant dengan slug acak dan mengembalikannya.
//
// Setiap test memanggil ini agar punya ruang sendiri di database bersama.
// Karena semua query agent dibatasi tenant_id, test yang berjalan beriringan
// tidak akan saling melihat data — sekaligus menjadi latihan atas sifat
// isolasi tenant yang memang ingin dibuktikan.
func newTenant(t *testing.T, db *gorm.DB) domain.Tenant {
	t.Helper()

	tenant := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "t" + uuid.New().String()[:8],
		Name:   "Test Tenant",
		Status: domain.TenantActive,
	}
	if err := tenant.Validate(); err != nil {
		t.Fatalf("test tenant is invalid: %v", err)
	}

	store := NewTenantStore(db)
	if err := store.Create(context.Background(), &tenant); err != nil {
		t.Fatalf("create test tenant: %v", err)
	}
	return tenant
}
