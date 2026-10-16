package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"agentmesh/internal/domain"
	"agentmesh/internal/store"
)

// newAgent membangun agent valid milik tenant tertentu, dengan nama acak
// agar aman dipakai di database bersama.
func newAgent(tenantID uuid.UUID, caps ...domain.Capability) domain.Agent {
	return domain.Agent{
		ID:           uuid.New(),
		TenantID:     tenantID,
		Name:         "a" + uuid.New().String()[:8],
		Endpoint:     "dns:///example:9090",
		Status:       domain.AgentUnknown,
		Capabilities: caps,
	}
}

func cap1(name, version string) domain.Capability {
	return domain.Capability{ID: uuid.New(), Name: name, Version: version}
}

func TestAgentStore_CreateAndGetWithCapabilities(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	want := newAgent(tenant.ID,
		cap1("security-review", "v1"),
		cap1("threat-model", "v2"),
	)

	if err := s.Create(ctx, &want); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}
	if want.CreatedAt.IsZero() {
		t.Error("Create() left CreatedAt zero")
	}

	got, err := s.GetByName(ctx, tenant.ID, want.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Endpoint != want.Endpoint {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, want.Endpoint)
	}
	if got.Status != domain.AgentUnknown {
		t.Errorf("Status = %q, want %q", got.Status, domain.AgentUnknown)
	}
	if len(got.Capabilities) != 2 {
		t.Fatalf("len(Capabilities) = %d, want 2", len(got.Capabilities))
	}

	// Kapabilitas harus kembali dalam urutan yang stabil, agar response API
	// tidak berubah-ubah antar permintaan untuk data yang sama.
	byName := map[string]string{}
	for _, c := range got.Capabilities {
		byName[c.Name] = c.Version
		if c.AgentID != want.ID {
			t.Errorf("capability %q has AgentID %v, want %v", c.Name, c.AgentID, want.ID)
		}
	}
	if byName["security-review"] != "v1" {
		t.Errorf("security-review version = %q, want v1", byName["security-review"])
	}
	if byName["threat-model"] != "v2" {
		t.Errorf("threat-model version = %q, want v2", byName["threat-model"])
	}
}

func TestAgentStore_CreateWithoutCapabilities(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	got, err := s.GetByName(ctx, tenant.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if len(got.Capabilities) != 0 {
		t.Errorf("len(Capabilities) = %d, want 0", len(got.Capabilities))
	}
}

func TestAgentStore_CreateDuplicateNameInSameTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	first := newAgent(tenant.ID)
	if err := s.Create(ctx, &first); err != nil {
		t.Fatalf("first Create() = %v, want nil", err)
	}

	second := newAgent(tenant.ID)
	second.Name = first.Name

	err := s.Create(ctx, &second)

	if err == nil {
		t.Fatal("second Create() = nil, want ErrAlreadyExists")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("second Create() = %v, want it to wrap ErrAlreadyExists", err)
	}
}

// Properti multi-tenant paling dasar: nama yang sama sah di tenant berbeda.
func TestAgentStore_SameNameAllowedInDifferentTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	inA := newAgent(tenantA.ID)
	if err := s.Create(ctx, &inA); err != nil {
		t.Fatalf("create in tenant A = %v, want nil", err)
	}

	inB := newAgent(tenantB.ID)
	inB.Name = inA.Name
	if err := s.Create(ctx, &inB); err != nil {
		t.Fatalf("create same name in tenant B = %v, want nil", err)
	}
}

// Review Focus #4 dan pembuktian batas transaksi: kapabilitas kembar
// menabrak unique index di tengah transaksi, dan seluruh create harus
// dibatalkan — tidak boleh ada baris agent yang tertinggal.
func TestAgentStore_CreateRollsBackWhenCapabilityInsertFails(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID,
		cap1("security-review", "v1"),
		cap1("security-review", "v1"), // duplikat: melanggar unique index
	)

	err := s.Create(ctx, &agent)

	if err == nil {
		t.Fatal("Create() = nil, want an error from the duplicate capability")
	}
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Errorf("Create() = %v, want it to wrap ErrAlreadyExists", err)
	}

	// Inti test ini: barisnya harus benar-benar hilang, bukan sekadar
	// error yang dikembalikan.
	if _, err := s.GetByName(ctx, tenant.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("GetByName() after failed create = %v, want ErrNotFound — the transaction did not roll back", err)
	}
}

// Properti isolasi tenant yang paling penting di seluruh plan ini.
func TestAgentStore_GetIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	_, err := s.GetByName(ctx, tenantB.ID, agent.Name)

	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("tenant B reading tenant A's agent = %v, want ErrNotFound", err)
	}
}

func TestAgentStore_DeleteIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenantB.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("tenant B deleting tenant A's agent = %v, want ErrNotFound", err)
	}

	// Agent milik tenant A harus tetap utuh.
	if _, err := s.GetByName(ctx, tenantA.ID, agent.Name); err != nil {
		t.Errorf("agent disappeared after a cross-tenant delete attempt: %v", err)
	}
}

func TestAgentStore_Update(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	agent.Endpoint = "dns:///moved:9999"
	agent.Status = domain.AgentHealthy
	if err := s.Update(ctx, &agent); err != nil {
		t.Fatalf("Update() = %v, want nil", err)
	}

	got, err := s.GetByName(ctx, tenant.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.Endpoint != "dns:///moved:9999" {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, "dns:///moved:9999")
	}
	if got.Status != domain.AgentHealthy {
		t.Errorf("Status = %q, want %q", got.Status, domain.AgentHealthy)
	}
	// Update hanya menyentuh endpoint dan status; kapabilitas tidak ikut
	// terhapus.
	if len(got.Capabilities) != 1 {
		t.Errorf("len(Capabilities) after update = %d, want 1", len(got.Capabilities))
	}
}

func TestAgentStore_UpdateIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenantA.ID)
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	impostor := agent
	impostor.TenantID = tenantB.ID
	impostor.Endpoint = "dns:///hijacked:1234"

	if err := s.Update(ctx, &impostor); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-tenant Update() = %v, want ErrNotFound", err)
	}

	got, err := s.GetByName(ctx, tenantA.ID, agent.Name)
	if err != nil {
		t.Fatalf("GetByName() = %v, want nil", err)
	}
	if got.Endpoint == "dns:///hijacked:1234" {
		t.Error("a cross-tenant Update() modified the agent — tenant scoping is broken")
	}
}

func TestAgentStore_Delete(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	agent := newAgent(tenant.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &agent); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	if err := s.Delete(ctx, tenant.ID, agent.Name); err != nil {
		t.Fatalf("Delete() = %v, want nil", err)
	}

	if _, err := s.GetByName(ctx, tenant.ID, agent.Name); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetByName() after delete = %v, want ErrNotFound", err)
	}

	// Kapabilitas harus ikut terhapus lewat ON DELETE CASCADE.
	var remaining int64
	if err := db.Table("capabilities").Where("agent_id = ?", agent.ID).Count(&remaining).Error; err != nil {
		t.Fatalf("count capabilities: %v", err)
	}
	if remaining != 0 {
		t.Errorf("capabilities remaining after agent delete = %d, want 0", remaining)
	}
}

// Query yang akan dipakai router di Phase 3.
func TestAgentStore_ListFiltersByCapability(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	reviewer := newAgent(tenant.ID, cap1("security-review", "v1"))
	planner := newAgent(tenant.ID, cap1("plan-work", "v1"))
	both := newAgent(tenant.ID, cap1("security-review", "v1"), cap1("plan-work", "v1"))

	for i, a := range []*domain.Agent{&reviewer, &planner, &both} {
		if err := s.Create(ctx, a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
	}

	items, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Capability: "security-review"})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	found := map[uuid.UUID]bool{}
	for _, a := range items {
		found[a.ID] = true
	}
	if !found[reviewer.ID] {
		t.Error("reviewer agent missing from capability-filtered list")
	}
	if !found[both.ID] {
		t.Error("multi-capability agent missing from capability-filtered list")
	}
	if found[planner.ID] {
		t.Error("planner agent appeared in a list filtered by a capability it does not have")
	}

	// Agent yang punya dua kapabilitas tidak boleh muncul dua kali hanya
	// karena join menghasilkan dua baris.
	seen := map[uuid.UUID]int{}
	for _, a := range items {
		seen[a.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("agent %v appeared %d times in one page, want exactly 1", id, n)
		}
	}
}

func TestAgentStore_ListFiltersByStatus(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	healthy := newAgent(tenant.ID)
	healthy.Status = domain.AgentHealthy
	unknown := newAgent(tenant.ID)

	for i, a := range []*domain.Agent{&healthy, &unknown} {
		if err := s.Create(ctx, a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
	}

	items, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Status: string(domain.AgentHealthy)})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}

	for _, a := range items {
		if a.Status != domain.AgentHealthy {
			t.Errorf("agent %q has status %q in a healthy-filtered list", a.Name, a.Status)
		}
	}

	found := false
	for _, a := range items {
		if a.ID == healthy.ID {
			found = true
		}
		if a.ID == unknown.ID {
			t.Error("unknown-status agent appeared in a healthy-filtered list")
		}
	}
	if !found {
		t.Error("healthy agent missing from healthy-filtered list")
	}
}

func TestAgentStore_ListIsScopedToTenant(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenantA := newTenant(t, db)
	tenantB := newTenant(t, db)
	s := NewAgentStore(db)

	inA := newAgent(tenantA.ID, cap1("security-review", "v1"))
	if err := s.Create(ctx, &inA); err != nil {
		t.Fatalf("Create() = %v, want nil", err)
	}

	items, _, err := s.List(ctx, tenantB.ID, store.AgentFilter{})
	if err != nil {
		t.Fatalf("List() = %v, want nil", err)
	}
	for _, a := range items {
		if a.ID == inA.ID {
			t.Fatal("tenant B's list contained tenant A's agent — tenant scoping is broken")
		}
	}
}

func TestAgentStore_ListPaginates(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	const total = 5
	created := make([]uuid.UUID, 0, total)
	for i := 0; i < total; i++ {
		a := newAgent(tenant.ID, cap1("shared-capability", "v1"))
		if err := s.Create(ctx, &a); err != nil {
			t.Fatalf("Create() #%d = %v, want nil", i, err)
		}
		created = append(created, a.ID)
		time.Sleep(2 * time.Millisecond)
	}

	seen := map[uuid.UUID]int{}
	cursor := ""
	pages := 0
	for {
		items, next, err := s.List(ctx, tenant.ID, store.AgentFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List() = %v, want nil", err)
		}
		if len(items) > 2 {
			t.Fatalf("List() returned %d items, want at most 2", len(items))
		}
		for _, a := range items {
			seen[a.ID]++
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

	for i, id := range created {
		if seen[id] != 1 {
			t.Errorf("agent #%d appeared %d times across all pages, want exactly 1", i, seen[id])
		}
	}
}

// Review Focus #2 di lapisan store: cursor rusak harus menjadi ErrInvalid.
func TestAgentStore_ListRejectsBadCursor(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	_, _, err := s.List(ctx, tenant.ID, store.AgentFilter{Cursor: "!!!garbage!!!"})

	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("List() with a bad cursor = %v, want ErrInvalid", err)
	}
}

// Dua permintaan create identik yang berjalan bersamaan: tepat satu boleh
// berhasil. Yang memutuskan adalah unique index, bukan kode aplikasi.
func TestAgentStore_ConcurrentCreateYieldsExactlyOneWinner(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	tenant := newTenant(t, db)
	s := NewAgentStore(db)

	name := "race" + uuid.New().String()[:8]

	const attempts = 2
	var wg sync.WaitGroup
	errs := make([]error, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := newAgent(tenant.ID, cap1("security-review", "v1"))
			a.Name = name
			errs[i] = s.Create(ctx, &a)
		}(i)
	}
	wg.Wait()

	var succeeded, conflicted int
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrAlreadyExists):
			conflicted++
		default:
			t.Errorf("attempt %d returned an unexpected error: %v", i, err)
		}
	}
	if succeeded != 1 {
		t.Errorf("successful creates = %d, want exactly 1", succeeded)
	}
	if conflicted != 1 {
		t.Errorf("conflicting creates = %d, want exactly 1", conflicted)
	}
}
