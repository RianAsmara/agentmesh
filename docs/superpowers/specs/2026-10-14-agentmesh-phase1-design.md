# AgentMesh — Phase 1 Design (Agent Registry, Control Plane, REST API)

Date: 2026-10-14
Status: Approved
Phase: 1 of 12

## Purpose

Phase 0 bootstrapped a process with a `/healthz` endpoint and no memory.
Phase 1 makes the control plane hold state: PostgreSQL persistence, a
domain model of tenants, agents, and capabilities, and a versioned REST
API to manage them.

This is a learning-first portfolio project. Every decision below is made
for a reason the implementer should be able to defend, and those reasons
are recorded here and expanded in `docs/learning-guide.md` once the code
exists.

## Success Criteria

1. `make dev && make migrate && make seed && make run-control-plane` yields
   a running control plane backed by PostgreSQL with demo data.
2. An operator can create, read, update, list, and delete tenants and
   agents over `/v1/...`, and the data survives a process restart.
3. `GET /v1/tenants/{slug}/agents?capability=security-review` returns the
   agents Phase 3's router will need — the router's lookup, exercised
   early through a human-testable interface.
4. Tenant A cannot read or mutate tenant B's agents.
5. `docs/learning-guide.md` gains a Phase 1 section following the Phase 0
   structure.

## Decisions Already Fixed by Prior ADRs

- PostgreSQL is the single source of truth (ADR 0002).
- REST/JSON at `/v1/...` for management; gRPC is reserved for the data
  plane (ADR 0007).
- Authorization goes behind an `authorization.Decider` interface, RBAC
  first (ADR 0006). Phase 1 implements none of it but must not foreclose
  it.

## Decisions Made in This Phase

| Decision | Choice | Recorded in |
|---|---|---|
| Database access | GORM | ADR 0008 (written in Task 10) |
| Capability modelling | Separate `capabilities` table | this spec |
| Agent registration | Operator-declared CRUD | this spec |
| Dev/test database | Docker Compose + testcontainers | this spec |
| Code layering | `api` → `store` → `domain` (ports and adapters) | this spec |

### On GORM and ADR 0002

ADR 0002 names PostgreSQL Row-Level Security as the tenant-isolation
backstop. GORM's connection pooling makes the usual RLS driver
(`SET LOCAL app.tenant_id` inside a transaction) awkward, because the
session variable must be set on the same pooled connection as the query.

Mitigation, which is load-bearing for this whole design: GORM is confined
to `internal/store/postgres`. Handlers depend only on the store
interfaces and never see a `*gorm.DB`. Phase 5 can therefore add a
transaction-scoped RLS wrapper, or replace a single store implementation
with hand-written SQL, without touching a line of handler code.

## Section 1 — Domain Model

`internal/domain` imports the standard library and `uuid` only. No GORM,
no `net/http`.

```go
type Tenant struct {
    ID        uuid.UUID
    Slug      string       // "acme" — URL-safe, immutable, globally unique
    Name      string       // "Acme Corp" — display name, mutable
    Status    TenantStatus // active | suspended
    CreatedAt, UpdatedAt time.Time
}

type Agent struct {
    ID           uuid.UUID
    TenantID     uuid.UUID
    Name         string      // "security-agent" — unique within tenant
    Endpoint     string      // "dns:///security:9090" — where Phase 3 dials
    Status       AgentStatus // unknown | healthy | unhealthy
    Capabilities []Capability
    CreatedAt, UpdatedAt time.Time
}

type Capability struct {
    ID        uuid.UUID
    AgentID   uuid.UUID
    Name      string      // "security-review" — what the router matches
    Version   string      // "v1"
    CreatedAt time.Time
}
```

**Slug separate from Name.** URLs need a stable, URL-safe, human-readable
identifier (`/v1/tenants/acme/agents`). Display names change; identifiers
in URLs must not.

**Status starts at `unknown`.** Phase 1 has no health signal, because
agents are operator-declared and heartbeats arrive in Phase 2. Defaulting
to `healthy` would make Phase 3's router dial agents that were never
observed to be alive. This declared-versus-observed split is the same one
Phase 6's drift detection is built on.

**Validation lives in the domain**, as `Validate() error` methods, so the
rules are testable with no database and no HTTP:

| Field | Rule |
|---|---|
| `Tenant.Slug` | 3–63 chars, `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, immutable after create |
| `Tenant.Name` | 1–200 chars, non-blank |
| `Agent.Name` | 1–63 chars, same charset as slug, unique per tenant |
| `Agent.Endpoint` | non-empty, parses as a gRPC target |
| `Capability.Name` | 1–63 chars, same charset |
| `Capability.Version` | `^v[0-9]+$` |

**Sentinel errors** in `domain`: `ErrNotFound`, `ErrAlreadyExists`,
`ErrInvalid`. The store translates database errors into these; the API
maps these to HTTP status codes. Neither layer learns the other's
vocabulary.

## Section 2 — Database Schema

```sql
CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agents (
    id         UUID PRIMARY KEY,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    endpoint   TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'unknown',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE capabilities (
    id         UUID PRIMARY KEY,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    version    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, name, version)
);

CREATE INDEX idx_capabilities_name     ON capabilities (name);
CREATE INDEX idx_agents_tenant_status  ON agents (tenant_id, status);
```

**`UNIQUE (tenant_id, name)`, not `UNIQUE (name)`** — multi-tenancy
expressed in the schema. Two tenants may each have a `security-agent`;
one tenant may not have two. The database enforces it, so concurrent
`POST`s cannot both succeed — something application-level "check then
insert" can never guarantee.

**`ON DELETE CASCADE`** — deleting a tenant removes its agents, which
removes their capabilities. The alternative is orphan rows or manual
cleanup, both reliable sources of production bugs.

**`TIMESTAMPTZ`, never `TIMESTAMP`** — `TIMESTAMP` has no timezone and
silently means "whatever the server thinks local time is," which differs
between a container, a laptop, and CI.

**UUIDs generated in Go, not by Postgres** — the ID exists before the
insert, so it can be logged, returned, and referenced within the same
transaction without a round-trip. Trade-off: random UUIDv4 fragments
B-tree locality; the production answer is time-ordered UUIDv7. Irrelevant
at this scale, worth a learning-guide note.

**Migrations are numbered `.sql` files** in
`internal/migrate/migrations/`, embedded with `go:embed`, applied by a
small runner tracking a `schema_migrations` table. Explicitly **not**
GORM's `AutoMigrate`: it cannot reliably express `ON DELETE CASCADE`,
never drops or renames, and leaves no reviewable record of what changed.

## Section 3 — REST API

```
POST   /v1/tenants                            201 → Tenant
GET    /v1/tenants                            200 → {items, next_cursor}
GET    /v1/tenants/{slug}                     200 → Tenant
PATCH  /v1/tenants/{slug}                     200 → Tenant   (name, status)
DELETE /v1/tenants/{slug}                     204

POST   /v1/tenants/{slug}/agents              201 → Agent    (+capabilities, one txn)
GET    /v1/tenants/{slug}/agents              200 → {items, next_cursor}
       ?capability=security-review &status=healthy &limit= &cursor=
GET    /v1/tenants/{slug}/agents/{name}       200 → Agent
PATCH  /v1/tenants/{slug}/agents/{name}       200 → Agent    (endpoint, status)
DELETE /v1/tenants/{slug}/agents/{name}       204

GET    /healthz                               200      liveness (exists in Phase 0)
GET    /readyz                                200/503  readiness — pings the database
```

**Agents nest under tenants** because an agent cannot exist without one,
and the tenant in the path is what every query scopes by. In Phase 5 that
path segment becomes the authorization boundary: one middleware resolves
`{slug}` to a tenant and checks the caller may act on it, instead of every
handler remembering to filter.

**Resources are addressed by name, not UUID** — greppable in logs,
typeable in `curl`. UUIDs still exist and are returned in responses.

**`?capability=`** is the exact query Phase 3's router needs, exercised
early through an interface a human can test.

**`/readyz` is distinct from `/healthz`** — liveness means "the process is
alive, do not restart me"; readiness means "I can serve traffic." A
control plane that has lost its database cannot serve traffic but should
not be killed. Conflating the two is how services enter restart loops
during a database blip.

**One error envelope everywhere**, so a CLI and a dashboard parse failures
uniformly:

```json
{ "error": { "code": "already_exists",
             "message": "agent \"security-agent\" already exists in tenant \"acme\"",
             "details": [{"field": "name", "issue": "must be unique within tenant"}] } }
```

| Condition | HTTP |
|---|---|
| `domain.ErrInvalid`, JSON decode failure | 400 |
| `domain.ErrNotFound` | 404 |
| `domain.ErrAlreadyExists` | 409 |
| anything else | 500, logged with detail, **generic message to the client** |

The last row matters: returning `pq: duplicate key value violates unique
constraint "idx_agents..."` to a client hands an attacker the schema.

## Section 4 — Package Layout

```
cmd/control-plane/main.go        wiring and server lifecycle

internal/
  domain/          tenant.go agent.go capability.go errors.go
                   imports: stdlib + uuid ONLY

  store/           store.go       TenantStore, AgentStore interfaces
    postgres/      db.go          connection and pool configuration
                   models.go      GORM structs and domain mapping
                   tenant.go      TenantStore implementation
                   agent.go       AgentStore implementation
                   errors.go      database error → domain sentinel

  migrate/         migrate.go     runner and schema_migrations
    migrations/    0001_init.sql  (go:embed)

  api/             router.go      mux and middleware chain
                   tenant.go      tenant handlers
                   agent.go       agent handlers
                   dto.go         request/response types and mapping
                   errors.go      domain error → HTTP response
                   middleware.go  recovery, request ID, logging

  config/          extended with DatabaseURL, pool settings
```

**The dependency rule:** `api → store → domain`, never backwards.
`domain` imports almost nothing; `store/postgres` is the only package that
may import GORM; `api` is the only package that may import `net/http`.
Enforceable later with `golangci-lint`'s `depguard`.

```go
type AgentStore interface {
    Create(ctx context.Context, a *domain.Agent) error
    GetByName(ctx context.Context, tenantID uuid.UUID, name string) (*domain.Agent, error)
    List(ctx context.Context, tenantID uuid.UUID, f AgentFilter) ([]domain.Agent, string, error)
    Update(ctx context.Context, a *domain.Agent) error
    Delete(ctx context.Context, tenantID uuid.UUID, name string) error
}
```

Note the absences: no `*gorm.DB`, no `Preload`, no SQL. A handler holding
this interface cannot tell what is behind it.

**No service layer in Phase 1.** Approach C (an `internal/service` layer
between `api` and `store`) is correct for where this project ends up —
Phase 4's snapshot creation and Phase 5's authorization both want that
seam. In Phase 1 nearly every method would be a one-line pass-through, so
it is deferred to the phase where it earns its place (YAGNI).

## Section 5 — Transaction Boundary

Creating an agent writes to two tables; both must land or neither.

```go
func (s *AgentStore) Create(ctx context.Context, a *domain.Agent) error {
    return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        m := toModel(a)
        if err := tx.Create(&m).Error; err != nil {
            return translateErr(err)
        }
        for _, c := range a.Capabilities {
            cm := toCapModel(c, m.ID)
            if err := tx.Create(&cm).Error; err != nil {
                return translateErr(err)
            }
        }
        return nil
    })
}
```

**The transaction lives in the store, not the handler.** The moment a
handler knows transactions exist it needs a `*gorm.DB`, and the seam
collapses. Atomicity is part of what "create an agent" means, so it
belongs with that operation's implementation.

**No check-then-insert.** Two concurrent requests both pass the check,
then both insert. The `UNIQUE (tenant_id, name)` index is what actually
prevents the duplicate: attempt the insert, translate the violation. Let
the database arbitrate races — it is the only component that can.

```go
func translateErr(err error) error {
    var pgErr *pgconn.PgError
    if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
        return domain.ErrAlreadyExists
    }
    if errors.Is(err, gorm.ErrRecordNotFound) {
        return domain.ErrNotFound
    }
    return err
}
```

This file is the entire membrane between database vocabulary and domain
vocabulary. `23505` is PostgreSQL's `unique_violation` SQLSTATE.

## Section 6 — Pagination

Cursor-based:

```
GET /v1/tenants/acme/agents?limit=50&cursor=eyJpZCI6...
→ { "items": [...], "next_cursor": "eyJpZCI6..." }
```

**Not `?page=3&size=50`.** Offset pagination makes the database count and
discard rows — `OFFSET 10000` reads 10,050 rows to return 50 — and if a
row is inserted mid-pagination every later page shifts, silently skipping
items. Cursor pagination carries the last-seen sort key
(`WHERE (created_at, id) > ($1, $2) ORDER BY created_at, id LIMIT $3`),
which is index-seekable and stable under concurrent writes.

The cursor is base64-encoded JSON of the sort key, opaque to clients by
design so the sort can change without breaking them. `limit` defaults to
50 and is capped at 200.

## Section 7 — Middleware

```
request → recovery → requestID → logging → handler
```

- **recovery** — a panic in one handler must not take down the process;
  convert to 500, log the stack.
- **requestID** — read `X-Request-ID` or generate one; place it in the
  context and the response header. Every log line for the request carries
  it. Phase 8's distributed tracing follows this thread.
- **logging** — one structured line per request: method, path, status,
  duration, request ID, using the Phase 0 `slog` logger.

The logger reaches handlers through `context`, keeping function signatures
clean. This answers the open question left in the Phase 0 learning guide's
structured-logging entry.

## Section 8 — Testing Strategy

Real PostgreSQL via testcontainers-go.

| Layer | What is tested | Speed |
|---|---|---|
| `domain` | validation rules, pure functions — no DB, no HTTP | instant |
| `store/postgres` | real SQL: constraints, cascades, tenant scoping, pagination, error translation | ~10s |
| `api` | status codes, error envelopes, routing, DTO mapping, against a real store | ~10s |
| `migrate` | migrations apply from empty; re-running is a no-op | ~5s |

One container per package, reused across that package's tests, each test
working in its own tenant for isolation. `make test` runs everything;
`-short` skips container tests for a fast inner loop.

Tests specifically required, because they catch the bugs that matter:

1. An agent create whose capability insert fails leaves no agent row
   (proves the transaction).
2. Tenant A cannot `GET` tenant B's agent by name (proves scoping — the
   single most important multi-tenant property).
3. Deleting a tenant removes its agents and their capabilities (proves the
   cascade).
4. Two concurrent identical creates yield exactly one 201 and one 409
   (proves the race is settled by the index).

## Section 9 — Local Development

`docker-compose.yml` with PostgreSQL 17 and a healthcheck, pulled forward
from Phase 8 because the database cannot be run locally otherwise. The
Makefile's `dev` target changes from a Phase 8 stub to a working target;
`migrate` and `seed` likewise.

```bash
make dev                 # docker compose up -d postgres
make migrate             # apply migrations
make seed                # tenant "acme" + planner/backend/security agents
make run-control-plane
```

`make seed` creates exactly the demo scenario the README promises, so
Phase 3's end-to-end demo has data waiting for it.

## Section 10 — Task Breakdown

| # | Task | Deliverable |
|---|---|---|
| 1 | Domain model | entities, validation, sentinel errors, tests |
| 2 | Migrations | runner, `0001_init.sql`, working `make migrate` |
| 3 | Store interfaces and GORM setup | interfaces, connection, models, error translation |
| 4 | TenantStore | implementation and testcontainer tests |
| 5 | AgentStore | implementation including transaction, filters, pagination, tests |
| 6 | Middleware | recovery, request ID, logging, tests |
| 7 | Tenant handlers | CRUD, DTOs, error mapping, tests |
| 8 | Agent handlers | CRUD, `?capability=`, `?status=`, tests |
| 9 | Wiring and `/readyz` | main.go, config extension, compose, seed |
| 10 | Documentation | learning-guide Phase 1 section, ADR 0008, README and architecture updates |

ADR 0008 records the GORM decision honestly: what it buys, the tension
with ADR 0002's RLS plan, and the store-interface mitigation. An ADR that
records only the upside is not worth writing.

## Out of Scope for Phase 1

Deliberately deferred, each to the phase that owns it:

- Authentication and authorization (Phase 5). Every endpoint in Phase 1 is
  unauthenticated; the README and learning guide must state this plainly
  rather than leave it implied.
- Heartbeats and real health status (Phase 2).
- Routes and policies as first-class entities (Phase 4).
- `agentctl` CLI, OpenAPI specification generation, audit log.
- Row-Level Security (Phase 5), enabled by the store-interface seam above.
