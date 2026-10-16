// Package config loads AgentMesh service configuration from environment
// variables, applying development-friendly defaults and validating values
// at startup so misconfiguration fails fast instead of surfacing later.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds runtime configuration for the control-plane service.
type Config struct {
	// HTTPAddr is the address the control-plane HTTP server listens on.
	HTTPAddr string
	// LogLevel controls structured log verbosity: debug, info, warn, error.
	LogLevel string
	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish before forcing an exit.
	ShutdownTimeout time.Duration

	// DatabaseURL adalah DSN PostgreSQL. Tidak ada nilai default: tanpa
	// database, control plane tidak bisa melayani apa pun, jadi lebih baik
	// gagal saat start dengan pesan yang jelas daripada menyala lalu
	// menolak setiap permintaan.
	DatabaseURL string
	// MaxOpenConns membatasi total koneksi ke PostgreSQL.
	MaxOpenConns int
	// MaxIdleConns menahan sejumlah koneksi tetap terbuka.
	MaxIdleConns int
	// ConnMaxLifetime memaksa koneksi lama didaur ulang.
	ConnMaxLifetime time.Duration
}

const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second

	// Nilai default pool sengaja kecil. PostgreSQL memakai satu proses per
	// koneksi, jadi batas yang kebesaran justru membebani server alih-alih
	// mempercepatnya. Angka ini cocok untuk pengembangan lokal dan aman
	// dinaikkan lewat environment variable saat dibutuhkan.
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
)

// Load builds a Config from environment variables, applying defaults for
// anything unset. It returns an error if a set value is invalid.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnvOrDefault("AGENTMESH_HTTP_ADDR", defaultHTTPAddr),
		LogLevel:        getEnvOrDefault("AGENTMESH_LOG_LEVEL", defaultLogLevel),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if raw := os.Getenv("AGENTMESH_SHUTDOWN_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_SHUTDOWN_TIMEOUT %q: %w", raw, err)
		}
		cfg.ShutdownTimeout = d
	}

	cfg.DatabaseURL = os.Getenv("AGENTMESH_DATABASE_URL")

	cfg.MaxOpenConns = defaultMaxOpenConns
	if raw := os.Getenv("AGENTMESH_DB_MAX_OPEN_CONNS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_MAX_OPEN_CONNS %q: %w", raw, err)
		}
		cfg.MaxOpenConns = n
	}

	cfg.MaxIdleConns = defaultMaxIdleConns
	if raw := os.Getenv("AGENTMESH_DB_MAX_IDLE_CONNS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_MAX_IDLE_CONNS %q: %w", raw, err)
		}
		cfg.MaxIdleConns = n
	}

	cfg.ConnMaxLifetime = defaultConnMaxLifetime
	if raw := os.Getenv("AGENTMESH_DB_CONN_MAX_LIFETIME"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_DB_CONN_MAX_LIFETIME %q: %w", raw, err)
		}
		cfg.ConnMaxLifetime = d
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid AGENTMESH_LOG_LEVEL %q: must be one of debug, info, warn, error", c.LogLevel)
	}
	if c.HTTPAddr == "" {
		return fmt.Errorf("AGENTMESH_HTTP_ADDR must not be empty")
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("AGENTMESH_SHUTDOWN_TIMEOUT must be positive, got %s", c.ShutdownTimeout)
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("AGENTMESH_DATABASE_URL must be set")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("AGENTMESH_DB_MAX_OPEN_CONNS must be positive, got %d", c.MaxOpenConns)
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("AGENTMESH_DB_MAX_IDLE_CONNS must not be negative, got %d", c.MaxIdleConns)
	}
	// Idle lebih besar daripada open tidak berarti apa-apa: database/sql
	// akan memangkasnya diam-diam, dan konfigurasi yang diam-diam diabaikan
	// lebih membingungkan daripada yang ditolak.
	if c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("AGENTMESH_DB_MAX_IDLE_CONNS (%d) must not exceed AGENTMESH_DB_MAX_OPEN_CONNS (%d)",
			c.MaxIdleConns, c.MaxOpenConns)
	}
	if c.ConnMaxLifetime <= 0 {
		return fmt.Errorf("AGENTMESH_DB_CONN_MAX_LIFETIME must be positive, got %s", c.ConnMaxLifetime)
	}
	return nil
}

func getEnvOrDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
