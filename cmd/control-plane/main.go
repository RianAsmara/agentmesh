// cmd/control-plane/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agentmesh/internal/api"
	"agentmesh/internal/config"
	"agentmesh/internal/observability"
	"agentmesh/internal/store/postgres"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(logOut io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observability.NewLogger(logOut, "control-plane", cfg.LogLevel)

	// Koneksi database dibuka sebelum server mulai mendengarkan. Kalau
	// database tidak terjangkau saat start, lebih baik proses gagal dengan
	// pesan jelas daripada menyala lalu menolak setiap permintaan.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 15*time.Second)
	defer cancelConnect()

	db, err := postgres.Open(connectCtx, cfg.DatabaseURL, postgres.Options{
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	logger.Info("connected to database",
		"max_open_conns", cfg.MaxOpenConns,
		"max_idle_conns", cfg.MaxIdleConns,
	)

	// Perakitan dependensi terjadi di sini dan hanya di sini. Setiap
	// lapisan menerima apa yang dibutuhkannya lewat interface, sehingga
	// tidak ada package yang mencari sendiri dependensinya lewat variabel
	// global.
	handler := api.NewRouter(api.RouterConfig{
		Logger:  logger,
		Tenants: postgres.NewTenantStore(db),
		Agents:  postgres.NewAgentStore(db),
		DB:      postgres.NewPinger(db),
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting control-plane HTTP server", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("control-plane stopped cleanly")
	return nil
}
