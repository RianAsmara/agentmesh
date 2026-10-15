package migrate

import (
	"context"
	"database/sql"
	"testing"
)

func TestRun_AppliesSchemaFromEmpty(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	for _, table := range []string{"tenants", "agents", "capabilities", "schema_migrations"} {
		var exists bool
		err := db.QueryRowContext(ctx,
			`SELECT EXISTS (SELECT 1 FROM information_schema.tables
			                WHERE table_schema = 'public' AND table_name = $1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("query information_schema for %q: %v", table, err)
		}
		if !exists {
			t.Errorf("table %q does not exist after Run()", table)
		}
	}
}

// Menjalankan migrasi dua kali harus aman. Ini kasus nyata: setiap pod yang
// restart akan memanggil Run lagi.
func TestRun_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	if err := Run(ctx, db); err != nil {
		t.Fatalf("first Run() = %v, want nil", err)
	}
	if err := Run(ctx, db); err != nil {
		t.Fatalf("second Run() = %v, want nil", err)
	}

	applied, err := Applied(ctx, db)
	if err != nil {
		t.Fatalf("Applied() = %v, want nil", err)
	}
	if len(applied) != 1 {
		t.Fatalf("Applied() returned %d versions (%v), want exactly 1", len(applied), applied)
	}
	if applied[0] != "0001_init" {
		t.Errorf("Applied()[0] = %q, want %q", applied[0], "0001_init")
	}
}

// Memastikan constraint yang dijanjikan spec benar-benar ada di schema.
func TestRun_EnforcesSchemaConstraints(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if err := Run(ctx, db); err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	mustExec(t, db, `INSERT INTO tenants (id, slug, name, status)
	                 VALUES ('11111111-1111-1111-1111-111111111111', 'acme', 'Acme', 'active')`)
	mustExec(t, db, `INSERT INTO tenants (id, slug, name, status)
	                 VALUES ('22222222-2222-2222-2222-222222222222', 'globex', 'Globex', 'active')`)

	t.Run("agent name is unique within a tenant", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                 VALUES ('aaaaaaaa-0000-0000-0000-000000000001',
		                         '11111111-1111-1111-1111-111111111111',
		                         'security-agent', 'dns:///s:9090', 'unknown')`)

		_, err := db.ExecContext(ctx, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                               VALUES ('aaaaaaaa-0000-0000-0000-000000000002',
		                                       '11111111-1111-1111-1111-111111111111',
		                                       'security-agent', 'dns:///s:9091', 'unknown')`)
		if err == nil {
			t.Fatal("duplicate agent name within tenant was accepted, want unique violation")
		}
	})

	t.Run("the same agent name is allowed in a different tenant", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO agents (id, tenant_id, name, endpoint, status)
		                 VALUES ('aaaaaaaa-0000-0000-0000-000000000003',
		                         '22222222-2222-2222-2222-222222222222',
		                         'security-agent', 'dns:///s:9090', 'unknown')`)
	})

	t.Run("deleting a tenant cascades to agents and capabilities", func(t *testing.T) {
		mustExec(t, db, `INSERT INTO capabilities (id, agent_id, name, version)
		                 VALUES ('cccccccc-0000-0000-0000-000000000001',
		                         'aaaaaaaa-0000-0000-0000-000000000001',
		                         'security-review', 'v1')`)

		mustExec(t, db, `DELETE FROM tenants WHERE id = '11111111-1111-1111-1111-111111111111'`)

		var agents, caps int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM agents
		    WHERE tenant_id = '11111111-1111-1111-1111-111111111111'`).Scan(&agents); err != nil {
			t.Fatalf("count agents: %v", err)
		}
		if agents != 0 {
			t.Errorf("agents remaining after tenant delete = %d, want 0", agents)
		}
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM capabilities
		    WHERE agent_id = 'aaaaaaaa-0000-0000-0000-000000000001'`).Scan(&caps); err != nil {
			t.Fatalf("count capabilities: %v", err)
		}
		if caps != 0 {
			t.Errorf("capabilities remaining after tenant delete = %d, want 0", caps)
		}
	})

	t.Run("timestamps are timezone aware", func(t *testing.T) {
		var dataType string
		err := db.QueryRowContext(ctx,
			`SELECT data_type FROM information_schema.columns
			 WHERE table_name = 'tenants' AND column_name = 'created_at'`).Scan(&dataType)
		if err != nil {
			t.Fatalf("query column type: %v", err)
		}
		if dataType != "timestamp with time zone" {
			t.Errorf("tenants.created_at type = %q, want %q", dataType, "timestamp with time zone")
		}
	})
}

// mustExec menjalankan satu statement SQL dan menggagalkan test bila error.
// Dipakai untuk menyiapkan data, bukan untuk assertion.
func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query); err != nil {
		t.Fatalf("exec %.60s...: %v", query, err)
	}
}
