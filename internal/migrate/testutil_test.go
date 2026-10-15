package migrate

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx" ke database/sql
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// newTestDB menjalankan satu container PostgreSQL sungguhan dan
// mengembalikan koneksi ke database yang masih kosong.
//
// Kenapa PostgreSQL asli, bukan SQLite atau mock: yang diuji di sini justru
// perilaku khas PostgreSQL — ON DELETE CASCADE, unique violation dengan
// SQLSTATE 23505, dan tipe TIMESTAMPTZ. Database tiruan akan meluluskan
// migrasi yang sebenarnya rusak.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping container-backed test in -short mode")
	}

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
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping database: %v", err)
	}
	return db
}
