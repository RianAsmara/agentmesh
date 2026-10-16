// Package store mendefinisikan kontrak penyimpanan AgentMesh.
//
// Interface di sini sengaja tidak menyebut GORM, SQL, maupun tipe apa pun
// dari driver database. Itulah sekat yang membuat seluruh rancangan ini
// bekerja: handler HTTP hanya memegang interface, sehingga Phase 5 bisa
// menambahkan Row-Level Security lewat pembungkus transaksi, atau mengganti
// implementasi dengan SQL tulisan tangan, tanpa menyentuh satu baris pun di
// lapisan api.
package store

import (
	"context"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
)

// defaultLimit dan maxLimit membatasi ukuran halaman. Batas atas mencegah
// satu permintaan menarik seluruh tabel ke memori.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// TenantFilter mengatur paginasi daftar tenant.
type TenantFilter struct {
	// Limit adalah jumlah maksimum baris yang dikembalikan. Nilai 0 berarti
	// DefaultLimit.
	Limit int
	// Cursor adalah penanda posisi buram dari halaman sebelumnya. Kosong
	// berarti mulai dari awal.
	Cursor string
}

// AgentFilter mengatur penyaringan dan paginasi daftar agent.
type AgentFilter struct {
	// Capability menyaring agent yang menyediakan nama kapabilitas tertentu.
	// Kosong berarti tanpa penyaringan. Inilah query yang akan dipakai
	// router di Phase 3.
	Capability string
	// Status menyaring menurut status kesehatan agent. Kosong berarti semua.
	Status string
	Limit  int
	Cursor string
}

// TenantStore menyimpan dan mengambil tenant.
type TenantStore interface {
	// Create menyimpan tenant baru. Mengembalikan domain.ErrAlreadyExists
	// bila slug sudah dipakai.
	Create(ctx context.Context, t *domain.Tenant) error
	// GetBySlug mengambil satu tenant. Mengembalikan domain.ErrNotFound
	// bila tidak ada.
	GetBySlug(ctx context.Context, slug string) (*domain.Tenant, error)
	// List mengembalikan satu halaman tenant beserta cursor halaman
	// berikutnya. Cursor kosong berarti tidak ada halaman lagi.
	List(ctx context.Context, f TenantFilter) (items []domain.Tenant, nextCursor string, err error)
	// Update menyimpan perubahan pada name dan status. Slug tidak pernah
	// berubah karena ia adalah identitas di URL.
	Update(ctx context.Context, t *domain.Tenant) error
	// Delete menghapus tenant beserta seluruh agent dan kapabilitasnya
	// lewat ON DELETE CASCADE.
	Delete(ctx context.Context, slug string) error
}

// AgentStore menyimpan dan mengambil agent beserta kapabilitasnya.
//
// Setiap method menerima tenantID dan bukan hanya nama agent. Itu disengaja:
// tidak ada satu pun cara di interface ini untuk mengambil agent tanpa
// menyebut tenant-nya, sehingga kebocoran antar-tenant tidak bisa terjadi
// karena seseorang lupa menambahkan klausa WHERE.
type AgentStore interface {
	// Create menyimpan agent beserta seluruh kapabilitasnya dalam satu
	// transaksi. Mengembalikan domain.ErrAlreadyExists bila nama sudah
	// dipakai di tenant yang sama.
	Create(ctx context.Context, a *domain.Agent) error
	// GetByName mengambil satu agent beserta kapabilitasnya.
	GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Agent, error)
	// List mengembalikan satu halaman agent beserta cursor berikutnya.
	List(ctx context.Context, tenantID uuid.UUID, f AgentFilter) (items []domain.Agent, nextCursor string, err error)
	// Update menyimpan perubahan pada endpoint dan status.
	Update(ctx context.Context, a *domain.Agent) error
	// Delete menghapus agent beserta kapabilitasnya.
	Delete(ctx context.Context, tenantID uuid.UUID, name string) error
}
