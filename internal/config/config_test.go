package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// Ensure hermeticity: clear all AGENTMESH_* vars regardless of what the
	// ambient environment (e.g. a developer's shell) has exported. t.Setenv
	// registers automatic restoration via t.Cleanup; the subsequent
	// os.Unsetenv makes the variable genuinely absent for the test body.
	for _, key := range []string{"AGENTMESH_HTTP_ADDR", "AGENTMESH_LOG_LEVEL", "AGENTMESH_SHUTDOWN_TIMEOUT"} {
		t.Setenv(key, "x")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q) failed: %v", key, err)
		}
	}
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
	if cfg.LogLevel != defaultLogLevel {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, defaultLogLevel)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want %v", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
}

func TestLoad_OverridesFromEnv(t *testing.T) {
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("AGENTMESH_HTTP_ADDR", ":9090")
	t.Setenv("AGENTMESH_LOG_LEVEL", "debug")
	t.Setenv("AGENTMESH_SHUTDOWN_TIMEOUT", "5s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 5s", cfg.ShutdownTimeout)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	t.Setenv("AGENTMESH_LOG_LEVEL", "verbose")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid log level, got nil")
	}
}

func TestLoad_InvalidShutdownTimeout(t *testing.T) {
	t.Setenv("AGENTMESH_SHUTDOWN_TIMEOUT", "not-a-duration")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid shutdown timeout, got nil")
	}
}

func TestLoad_EmptyHTTPAddrFallsBackToDefault(t *testing.T) {
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("AGENTMESH_HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.HTTPAddr != defaultHTTPAddr {
		t.Errorf("HTTPAddr = %q, want default %q", cfg.HTTPAddr, defaultHTTPAddr)
	}
}

func TestLoad_EmptyShutdownTimeoutFallsBackToDefault(t *testing.T) {
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
	t.Setenv("AGENTMESH_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want default %v", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
}

func TestLoad_DatabaseURLRequired(t *testing.T) {
	for _, key := range []string{"AGENTMESH_DATABASE_URL"} {
		t.Setenv(key, "x")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("os.Unsetenv(%q) failed: %v", key, err)
		}
	}

	_, err := Load()

	if err == nil {
		t.Fatal("Load() = nil error, want an error when AGENTMESH_DATABASE_URL is unset")
	}
	if !strings.Contains(err.Error(), "AGENTMESH_DATABASE_URL") {
		t.Errorf("error %q does not name the missing variable", err)
	}
}

func TestLoad_PoolDefaults(t *testing.T) {
	t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() = %v, want nil", err)
	}
	if cfg.MaxOpenConns != defaultMaxOpenConns {
		t.Errorf("MaxOpenConns = %d, want %d", cfg.MaxOpenConns, defaultMaxOpenConns)
	}
	if cfg.MaxIdleConns != defaultMaxIdleConns {
		t.Errorf("MaxIdleConns = %d, want %d", cfg.MaxIdleConns, defaultMaxIdleConns)
	}
	if cfg.ConnMaxLifetime != defaultConnMaxLifetime {
		t.Errorf("ConnMaxLifetime = %v, want %v", cfg.ConnMaxLifetime, defaultConnMaxLifetime)
	}
}

func TestLoad_RejectsInvalidPoolSettings(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
	}{
		{"zero max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "0"},
		{"negative max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "-1"},
		{"non numeric max open conns", "AGENTMESH_DB_MAX_OPEN_CONNS", "many"},
		{"negative max idle conns", "AGENTMESH_DB_MAX_IDLE_CONNS", "-1"},
		{"idle above open", "AGENTMESH_DB_MAX_IDLE_CONNS", "999"},
		{"bad lifetime", "AGENTMESH_DB_CONN_MAX_LIFETIME", "forever"},
		{"zero lifetime", "AGENTMESH_DB_CONN_MAX_LIFETIME", "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
			t.Setenv(tt.key, tt.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() = nil error, want rejection of %s=%q", tt.key, tt.value)
			}
		})
	}
}

// Batas atas menangkap salah ketik yang kalau tidak akan muncul sebagai
// kegagalan koneksi acak jauh dari penyebabnya.
func TestLoad_RejectsOutOfRangePoolSettings(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"open conns above the ceiling", map[string]string{"AGENTMESH_DB_MAX_OPEN_CONNS": "5000"}},
		{"lifetime above the ceiling", map[string]string{"AGENTMESH_DB_CONN_MAX_LIFETIME": "720h"}},
		{"negative lifetime", map[string]string{"AGENTMESH_DB_CONN_MAX_LIFETIME": "-5s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AGENTMESH_DATABASE_URL", "postgres://u:p@localhost:5432/db")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Errorf("Load() = nil error, want a rejection for %v", tt.env)
			}
		})
	}
}
