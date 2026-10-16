// Command seed memuat data demo: satu tenant "acme" dengan tiga agent.
//
// Ini adalah skenario yang dijanjikan README dan yang akan dipakai demo
// end-to-end Phase 3, sehingga datanya sudah tersedia jauh sebelum router
// ada.
//
// Operasinya idempoten: menjalankan ulang tidak menggandakan apa pun, dan
// data yang sudah ada dibiarkan. Perintah seed yang gagal pada pemanggilan
// kedua adalah gangguan kecil yang menjengkelkan selama pengembangan.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("AGENTMESH_DATABASE_URL")
	if dsn == "" {
		return errors.New("AGENTMESH_DATABASE_URL must be set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, dsn, postgres.Options{
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Minute,
	})
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}()

	tenants := postgres.NewTenantStore(db)
	agents := postgres.NewAgentStore(db)

	tenant := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "acme",
		Name:   "Acme Corp",
		Status: domain.TenantActive,
	}
	if err := tenants.Create(ctx, &tenant); err != nil {
		if !errors.Is(err, domain.ErrAlreadyExists) {
			return fmt.Errorf("create tenant: %w", err)
		}
		// Sudah ada: ambil ID aslinya supaya agent di bawah menempel pada
		// tenant yang benar, bukan pada UUID yang baru saja dibuat.
		existing, getErr := tenants.GetBySlug(ctx, tenant.Slug)
		if getErr != nil {
			return fmt.Errorf("read existing tenant: %w", getErr)
		}
		tenant = *existing
		fmt.Printf("tenant %q already exists, reusing it\n", tenant.Slug)
	} else {
		fmt.Printf("created tenant %q\n", tenant.Slug)
	}

	specs := []struct {
		name         string
		endpoint     string
		capabilities []domain.Capability
	}{
		{
			name:     "planner-agent",
			endpoint: "dns:///planner-agent:9090",
			capabilities: []domain.Capability{
				{Name: "plan-work", Version: "v1"},
				{Name: "decompose-task", Version: "v1"},
			},
		},
		{
			name:     "backend-agent",
			endpoint: "dns:///backend-agent:9090",
			capabilities: []domain.Capability{
				{Name: "implement-endpoint", Version: "v1"},
				{Name: "write-migration", Version: "v1"},
			},
		},
		{
			name:     "security-agent",
			endpoint: "dns:///security-agent:9090",
			capabilities: []domain.Capability{
				{Name: "security-review", Version: "v1"},
			},
		},
	}

	for _, spec := range specs {
		agentID := uuid.New()
		caps := make([]domain.Capability, 0, len(spec.capabilities))
		for _, c := range spec.capabilities {
			caps = append(caps, domain.Capability{
				ID: uuid.New(), AgentID: agentID, Name: c.Name, Version: c.Version,
			})
		}

		agent := domain.Agent{
			ID:       agentID,
			TenantID: tenant.ID,
			Name:     spec.name,
			Endpoint: spec.endpoint,
			// Status unknown: belum ada agent sungguhan yang pernah
			// melapor. Phase 2 yang akan mengubahnya.
			Status:       domain.AgentUnknown,
			Capabilities: caps,
		}

		if err := agent.Validate(); err != nil {
			return fmt.Errorf("seed agent %q is invalid: %w", spec.name, err)
		}

		if err := agents.Create(ctx, &agent); err != nil {
			if errors.Is(err, domain.ErrAlreadyExists) {
				fmt.Printf("agent %q already exists, skipping\n", spec.name)
				continue
			}
			return fmt.Errorf("create agent %q: %w", spec.name, err)
		}
		fmt.Printf("created agent %q with %d capabilities\n", spec.name, len(caps))
	}

	return nil
}
