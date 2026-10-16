// cmd/control-plane/main_test.go
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// run harus gagal cepat dengan pesan yang menyebut variabel yang hilang,
// bukan panic dan bukan diam-diam menyala tanpa database.
func TestRun_FailsWithoutDatabaseURL(t *testing.T) {
	for _, key := range []string{"AGENTMESH_DATABASE_URL"} {
		t.Setenv(key, "x")
		if err := unsetenv(key); err != nil {
			t.Fatalf("unset %q: %v", key, err)
		}
	}

	err := run(&bytes.Buffer{})

	if err == nil {
		t.Fatal("run() = nil, want an error when AGENTMESH_DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "AGENTMESH_DATABASE_URL") {
		t.Errorf("error %q does not name the missing variable", err)
	}
}

// run harus mengembalikan error, bukan panic, ketika database tidak bisa
// dihubungi.
func TestRun_FailsWhenDatabaseUnreachable(t *testing.T) {
	// Port 1 dijamin tidak ada yang mendengarkan.
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=1")
	t.Setenv("AGENTMESH_LOG_LEVEL", "error")

	err := run(&bytes.Buffer{})

	if err == nil {
		t.Fatal("run() = nil, want an error when the database is unreachable")
	}
	if !strings.Contains(err.Error(), "connect to database") {
		t.Errorf("error %q does not explain that the database connection failed", err)
	}
}

// unsetenv membungkus os.Unsetenv agar test di atas terbaca jelas.
func unsetenv(key string) error { return os.Unsetenv(key) }
