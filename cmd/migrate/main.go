// Command migrate menerapkan migrasi skema AgentMesh lalu keluar.
//
// Dipisah dari control-plane sebagai binary tersendiri supaya migrasi bisa
// dijalankan sebagai langkah deploy terpisah — di Kubernetes nanti ini
// menjadi initContainer atau Job, bukan sesuatu yang dijalankan setiap
// replica saat start dan berebut mengunci tabel yang sama.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // mendaftarkan driver "pgx"

	"agentmesh/internal/migrate"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("AGENTMESH_DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	if err := migrate.Run(ctx, db); err != nil {
		return err
	}

	applied, err := migrate.Applied(ctx, db)
	if err != nil {
		return err
	}
	fmt.Printf("migrations applied: %d\n", len(applied))
	for _, v := range applied {
		fmt.Println("  -", v)
	}
	return nil
}
