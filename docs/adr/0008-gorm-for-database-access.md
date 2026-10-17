# ADR 0008: GORM for Database Access

Status: Accepted

## Context

Phase 1 introduces PostgreSQL persistence for tenants, agents, and
capabilities. The control plane needs a way to issue queries, map rows to
Go structs, and manage transactions.

Three options were considered: `pgx` with hand-written SQL, `sqlc` with
generated type-safe queries, and GORM as a full ORM.

## Decision

Use GORM (`gorm.io/gorm` with `gorm.io/driver/postgres`), confined entirely
to `internal/store/postgres`.

Schema changes are **not** managed by GORM. Migrations are numbered `.sql`
files embedded with `go:embed` and applied by `internal/migrate`.
`AutoMigrate` is never called.

## Alternatives Considered

- **pgx with hand-written SQL** — every query visible, no code generation,
  the closest fit to the project's stdlib-first preference. Rejected for
  Phase 1 in favour of development speed on what is largely CRUD.
- **sqlc** — real SQL with generated type-safe Go, no runtime ORM. Rejected
  to avoid a second code-generation step alongside `buf`.

## Consequences

GORM writes the SQL, which means the query plan is less visible than it
would be with hand-written statements. Two specific tensions follow, and
both are mitigated rather than ignored:

**Tension with ADR 0002 (Row-Level Security).** ADR 0002 names PostgreSQL
RLS as the tenant-isolation backstop. The usual way to drive RLS is
`SET LOCAL app.tenant_id` inside a transaction, which requires the session
variable and the query to run on the same pooled connection. GORM's
connection pooling makes that awkward: a `*gorm.DB` hands out whatever
connection is free, so a `SET LOCAL` issued on one borrowed connection has
no guarantee of being visible to the next statement GORM happens to route
to a different one, unless every statement in that logical unit is pinned
to the same `*gorm.Tx`.

*Mitigation:* GORM is confined to `internal/store/postgres`. Handlers
depend only on the `store.TenantStore` and `store.AgentStore` interfaces
and never see a `*gorm.DB`. Phase 5 can therefore add a transaction-scoped
RLS wrapper — issuing `SET LOCAL app.tenant_id` and the query inside the
same `Transaction(func(tx *gorm.DB) error { ... })` call already used by
`AgentStore.Create` — or replace a store implementation with hand-written
SQL, without touching a line of handler code. This confinement is verified
by a grep step in the Phase 1 implementation plan (`grep -rln 'gorm.io'
--include='*.go' . | grep -v '^./internal/store/postgres/'`), not merely by
convention. The tension is real today — RLS is not implemented in Phase 1,
and nothing currently exercises the pinned-connection path — but the seam
that would have to carry it already exists and is enforced mechanically.

**Tension with explicit schema control.** `AutoMigrate` cannot reliably
express `ON DELETE CASCADE`, never drops or renames, and leaves no
reviewable record of a change.

*Mitigation:* GORM is used for queries only. The schema belongs to the
`.sql` migration files, and the store tests run against a real PostgreSQL
instance migrated by those files — so a mismatch between the GORM models
and the real schema fails the test suite rather than production.

GORM also brings behavior that has to be managed deliberately:
`SkipDefaultTransaction` is set so writes are not silently wrapped in
transactions, and association auto-save is disabled with `Omit` where
capabilities are inserted explicitly, so that each insert's error can be
translated individually.
