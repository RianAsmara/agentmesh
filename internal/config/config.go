// Package config loads AgentMesh service configuration from environment
// variables, applying development-friendly defaults and validating values
// at startup so misconfiguration fails fast instead of surfacing later.
package config

import (
	"fmt"
	"os"
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
}

const (
	defaultHTTPAddr        = ":8080"
	defaultLogLevel        = "info"
	defaultShutdownTimeout = 10 * time.Second
)

// Load builds a Config from environment variables, applying defaults for
// anything unset. It returns an error if a set value is invalid.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:        getEnvOrDefault("AGENTMESH_HTTP_ADDR", defaultHTTPAddr),
		LogLevel:        getEnvOrDefault("AGENTMESH_LOG_LEVEL", defaultLogLevel),
		ShutdownTimeout: defaultShutdownTimeout,
	}

	if raw, ok := os.LookupEnv("AGENTMESH_SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid AGENTMESH_SHUTDOWN_TIMEOUT %q: %w", raw, err)
		}
		cfg.ShutdownTimeout = d
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
	return nil
}

func getEnvOrDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
