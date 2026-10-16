package postgres

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// Pinger memeriksa apakah database masih terjangkau. Dipakai oleh probe
// readiness di lapisan api.
//
// Tipe kecil ini ada supaya package api tidak perlu mengenal *gorm.DB sama
// sekali: ia hanya butuh sesuatu yang punya method Ping.
type Pinger struct {
	db *gorm.DB
}

// NewPinger membuat Pinger baru.
func NewPinger(db *gorm.DB) *Pinger {
	return &Pinger{db: db}
}

// Ping memastikan database membalas.
func (p *Pinger) Ping(ctx context.Context) error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return fmt.Errorf("access underlying sql.DB: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
