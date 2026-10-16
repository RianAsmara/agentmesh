package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

func TestTenantStore_CreateAndGet(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	want := domain.Tenant{
		ID:     uuid.New(),
		Slug:   "t" + uuid.New().String()[:8],
		Name:   "Acme Corp",
		Status: domain.TenantActive,
	}

	if err := s.Create(ctx, &want); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	// Create harus mengisi timestamp, karena pemanggil mengembalikannya
	// dalam response HTTP.
	if want.CreatedAt.IsZero() {
		t.Error("Create() left CreatedAt zero")
	}
	if want.UpdatedAt.IsZero() {
		t.Error("Create() left UpdatedAt zero")
	}

	got, err := s.GetBySlug(ctx, want.Slug)
	if err != nil {
		t.Fatalf("GetBySlug() = %v, want nil", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Slug != want.Slug {
		t.Errorf("Slug = %q, want %q", got.Slug, want.Slug)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	if got.Status != domain.TenantActive {
		t.Errorf("Status = %q, want %q", got.Status, domain.TenantActive)
	}
}

func TestTenantStore_CreateDuplicateSlug(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	slug := "t" + uuid.New().String()[:8]
	first := domain.Tenant{ID: uuid.New(), Slug: slug, Name: "First", Status: domain.TenantActive}
	if err := s.Create(ctx, &first); err != nil {
		t.Fatalf("first Create() = %v, want nil", err)
	}

	second := domain.Tenant{ID: uuid.New(), Slug: slug, Name: "Second", Status: domain.TenantActive}
	err := s.Create(ctx, &second)

	if err == nil {
		t.Fatal("second Create() = nil, want ErrAlreadyExists")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("second Create() = %v, want it to wrap ErrAlreadyExists", err)
	}
}

func TestTenantStore_GetMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	_, err := s.GetBySlug(ctx, "does-not-exist-"+uuid.New().String()[:8])

	if err == nil {
		t.Fatal("GetBySlug() = nil error, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetBySlug() = %v, want it to wrap ErrNotFound", err)
	}
}

func TestTenantStore_Update(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	tenant := domain.Tenant{
		ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
		Name: "Before", Status: domain.TenantActive,
	}
	if err := s.Create(ctx, &tenant); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	createdAt := tenant.CreatedAt

	// Jeda kecil agar perubahan UpdatedAt benar-benar terukur.
	time.Sleep(10 * time.Millisecond)

	tenant.Name = "After"
	tenant.Status = domain.TenantSuspended
	if err := s.Update(ctx, &tenant); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.GetBySlug(ctx, tenant.Slug)
	if err != nil {
		t.Fatalf("GetBySlug() = %v, want nil", err)
	}
	if got.Name != "After" {
		t.Errorf("Name = %q, want %q", got.Name, "After")
	}
	if got.Status != domain.TenantSuspended {
		t.Errorf("Status = %q, want %q", got.Status, domain.TenantSuspended)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Errorf("Update() changed CreatedAt: got %v, want %v", got.CreatedAt, createdAt)
	}
	if !got.UpdatedAt.After(createdAt) {
		t.Errorf("UpdatedAt = %v, want it after CreatedAt %v", got.UpdatedAt, createdAt)
	}
}

func TestTenantStore_UpdateMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	absent := domain.Tenant{
		ID: uuid.New(), Slug: "absent-" + uuid.New().String()[:8],
		Name: "Nobody", Status: domain.TenantActive,
	}

	err := s.Update(ctx, &absent)

	if err == nil {
		t.Fatal("Update() = nil, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update() = %v, want it to wrap ErrNotFound", err)
	}
}

func TestTenantStore_Delete(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	tenant := domain.Tenant{
		ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
		Name: "Doomed", Status: domain.TenantActive,
	}
	if err := s.Create(ctx, &tenant); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenant.Slug); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}

	if _, err := s.GetBySlug(ctx, tenant.Slug); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetBySlug() after delete = %v, want ErrNotFound", err)
	}
}

func TestTenantStore_DeleteMissing(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	err := s.Delete(ctx, "absent-"+uuid.New().String()[:8])

	if err == nil {
		t.Fatal("Delete() = nil, want ErrNotFound")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete() = %v, want it to wrap ErrNotFound", err)
	}
}

// Paginasi diuji dengan prefix slug unik agar tidak terganggu tenant milik
// test lain yang berbagi database yang sama.
func TestTenantStore_ListPaginates(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	const total = 5
	created := make([]domain.Tenant, 0, total)
	for i := 0; i < total; i++ {
		tenant := domain.Tenant{
			ID: uuid.New(), Slug: "t" + uuid.New().String()[:8],
			Name: "Page Test", Status: domain.TenantActive,
		}
		if err := s.Create(ctx, &tenant); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
		created = append(created, tenant)
		// Timestamp harus berbeda agar urutan (created_at, id) deterministik.
		time.Sleep(2 * time.Millisecond)
	}

	// Kumpulkan seluruh halaman dengan ukuran 2.
	seen := map[uuid.UUID]int{}
	cursor := ""
	pages := 0
	for {
		items, next, err := s.List(ctx, store.TenantFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(items) > 2 {
			t.Fatalf("List() returned %d items, want at most 2", len(items))
		}
		for _, item := range items {
			seen[item.ID]++
		}
		pages++
		if pages > 100 {
			t.Fatal("List() pagination did not terminate after 100 pages")
		}
		if next == "" {
			break
		}
		cursor = next
	}

	for i, tenant := range created {
		switch seen[tenant.ID] {
		case 1: // benar
		case 0:
			t.Errorf("tenant #%d (%s) never appeared in any page", i, tenant.Slug)
		default:
			t.Errorf("tenant #%d (%s) appeared %d times, want exactly 1", i, tenant.Slug, seen[tenant.ID])
		}
	}
}

func TestTenantStore_ListRejectsBadCursor(t *testing.T) {
	ctx := context.Background()
	s := NewTenantStore(testDB(t))

	_, _, err := s.List(ctx, store.TenantFilter{Cursor: "!!!not-a-cursor!!!"})

	if err == nil {
		t.Fatal("List() = nil error, want ErrInvalid")
	}
	if !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("List() = %v, want it to wrap ErrInvalid", err)
	}
}
