package config

import (
	"os"
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
	t.Setenv("AGENTMESH_SHUTDOWN_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("ShutdownTimeout = %v, want default %v", cfg.ShutdownTimeout, defaultShutdownTimeout)
	}
}
