# AgentMesh Phase 2 Implementation Plan (gRPC Contract, Example Agents, Heartbeats)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **ALSO REQUIRED:** Use the project skill `agentmesh-phase-dev` before writing any
> test double or dispatching any task. Five of Phase 1's defects lived in fakes that
> diverged from the real store, and four of those were prescribed by the brief itself.

**Goal:** Make `agents.status` a fact the system observed rather than a string an operator typed, by having agents heartbeat over gRPC and deriving health from the last arrival.

**Architecture:** Agents push; the control plane never dials out. A nullable `last_heartbeat_at` column records arrivals. Effective status is computed on read by comparing that timestamp against a pre-computed cutoff the caller passes in — no background sweeper, no clock inside the store.

**Tech Stack:** Go, PostgreSQL 17, GORM, gRPC + protobuf (buf), testcontainers.

**Spec:** `docs/superpowers/specs/2026-10-17-agentmesh-phase2-design.md`

## Global Constraints

Copied verbatim from the spec; every task's requirements include these.

- `internal/api` imports no `gorm` or `pgx`. Verify: `go list -deps ./internal/api | grep -E 'gorm|pgx'` must be empty. The gRPC service layer inherits this rule.
- Doc comments in Bahasa Indonesia; identifiers, log keys, and error-message strings in English.
- `WithRecovery` is innermost in any middleware chain. `TestRouter_PanicIsRecoveredWithRequestIDAndSummary` fails if reverted.
- `/healthz` must not touch the database; `/readyz` must reflect real reachability.
- Lint gate is `/tmp/golangci-v2-install/golangci-lint run ./...` and must print `0 issues.` The PATH binary is v1 and refuses this repo's v2 config. Reinstall: `GOBIN=/tmp/golangci-v2-install go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`
- Never name a test constant after a credential (`secret`, `pw`, `token`, `password`). gosec G101 matches the identifier name, not the value.
- Every task ends on a tree where `go build ./...` and `go test ./...` both pass.
- Count tests with: top-level `grep '^=== RUN   Test' <file> | grep -vc '/'`, subtests `grep '^=== RUN   Test' <file> | grep -c '/'`. Paste the output; do not report from memory.
- Phase 2 adds no new `AgentStatus` value. `unknown`, `healthy`, `unhealthy` already exist.
- Heartbeats are unauthenticated, like everything else in Phase 1. Do not add auth (Phase 5).

## Review Focus

Input classes the spec implies but no single task's happy path exercises. Each has a test assigned to the task owning the code.

1. **Clock skew — an agent whose `last_heartbeat_at` is in the future.** A machine with a fast clock, or a bad `now` in a test fixture. Expected: treated as healthy, never as an error or a negative-duration panic. Test in Task 4.
2. **`last_heartbeat_at` exactly at the cutoff.** Off-by-one here is the difference between flapping and never flipping. Expected: `>=` cutoff is healthy, strictly before is stale. Test in Task 4.
3. **A heartbeat for a tenant that exists and an agent name that does not** (and the reverse). Expected: `domain.ErrNotFound`, never an implicit row creation. Test in Task 3.
4. **A heartbeat arriving for an agent in another tenant with the same name.** Phase 1's `UNIQUE (tenant_id, name)` means two tenants may both have `security-agent`. Expected: each tenant's row updated independently; no cross-tenant write. Test in Task 3.
5. **An example agent started while the control plane is down, or surviving its restart.** Expected: heartbeat failures are logged and retried on the next tick, never fatal. Success Criterion 2 depends on this. Test in Task 7.

---

### Task 1: The gRPC agent contract

**Files:**
- Create: `api/proto/agentmesh/v1/agent.proto`
- Generated (by `buf generate`, committed): `api/proto/gen/go/agentmesh/v1/agent.pb.go`, `agent_grpc.pb.go`

**Interfaces:**
- Consumes: `agentmesh.v1.RequestMetadata` and `agentmesh.v1.ErrorDetail` from the existing `common.proto`.
- Produces: `AgentService` (agent-implemented) and `AgentRegistrationService` (control-plane-implemented) with `Heartbeat(HeartbeatRequest) returns (HeartbeatResponse)`. `HeartbeatRequest` fields: `RequestMetadata metadata`, `string tenant_slug`, `string agent_name`, `repeated Capability capabilities`. `HeartbeatResponse` fields: `google.protobuf.Duration next_heartbeat_after`. `Capability` message: `string name`, `string version`.

Two services in one file, with a comment at each stating its direction — who implements and who calls. Conflating them is the mistake this separation exists to prevent.

`AgentService` needs the capability-invocation RPC Phase 3 will call. Its payload shape is the implementer's choice, constrained to carrying `RequestMetadata` and returning `ErrorDetail` on failure. Phase 2 does not call it; the example agents implement it so the contract is proven by implementation.

`Heartbeat` is unary, not streaming — a half-open stream reads as healthy while the process behind it is wedged.

Address the agent by `tenant_slug` + `agent_name`, not UUID: Phase 1 fixed name-based addressing, and an agent process should not need to learn a UUID to report in.

- [ ] **Step 1: Write `agent.proto`**

- [ ] **Step 2: Lint and generate**

Run: `cd api/proto && buf lint && buf generate`
Expected: no lint output; generated files appear under `gen/go/agentmesh/v1/`.

- [ ] **Step 3: Verify the tree still builds**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add api/proto
git commit -m "feat: add gRPC agent contract with heartbeat RPC"
```

---

### Task 2: Migration and the ordering test

**Files:**
- Create: `internal/migrate/migrations/0002_heartbeats.sql`
- Modify: `internal/migrate/migrate_test.go`

**Interfaces:**
- Consumes: `migrate.Run(ctx, *sql.DB) error`, `migrate.Applied(ctx, *sql.DB) ([]string, error)`.
- Produces: `agents.last_heartbeat_at TIMESTAMPTZ NULL` and an index on it.

Nullable is load-bearing: `NULL` means "never heard from" (`unknown`), distinct from "heard from, long ago" (`unhealthy`). Collapsing those loses the distinction `unknown` exists to express.

Phase 1's review flagged that no test covers ordering across more than one migration file. This is the task that adds the second file, so it adds that test.

- [ ] **Step 1: Write the failing ordering test**

In `internal/migrate/migrate_test.go`, add `TestRun_AppliesMigrationsInFilenameOrder`: run `Run`, then assert `Applied` returns `["0001_init", "0002_heartbeats"]` in exactly that order.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/migrate/ -run TestRun_AppliesMigrationsInFilenameOrder -v`
Expected: FAIL — only `0001_init` is applied.

- [ ] **Step 3: Write `0002_heartbeats.sql`**

`ALTER TABLE agents ADD COLUMN last_heartbeat_at TIMESTAMPTZ;` plus an index on `last_heartbeat_at`. No `DEFAULT` — a default would make every existing row look freshly heartbeated. Comment why, in Indonesian, matching `0001_init.sql`'s density.

- [ ] **Step 4: Run the migrate tests**

Run: `go test ./internal/migrate/ -v`
Expected: PASS, including the new ordering test.

- [ ] **Step 5: Commit**

```bash
git add internal/migrate
git commit -m "feat: add last_heartbeat_at column and migration ordering test"
```

---

### Task 3: Store layer — persist a heartbeat

**Files:**
- Modify: `internal/store/store.go`, `internal/store/postgres/agent.go`, `internal/store/postgres/models.go`
- Test: `internal/store/postgres/agent_test.go`

**Interfaces:**
- Consumes: `testDB` and `newTenant` from `internal/store/postgres/testutil_test.go`; `translateErr`.
- Produces: `AgentStore.RecordHeartbeat(ctx context.Context, tenantID uuid.UUID, name string, at time.Time) error` added to the `store.AgentStore` interface and implemented on `*postgres.AgentStore`. Also `domain.Agent.LastHeartbeatAt *time.Time`.

A dedicated method, not a reuse of `Update`: `Update` carries endpoint and status from a REST PATCH, and overloading it would let a heartbeat silently clobber an operator's edit.

`at` is a parameter, not `time.Now()` inside the method — the caller owns the clock (see Task 4's cutoff reasoning). Truncate to microseconds before writing: Phase 1 established that PostgreSQL `TIMESTAMPTZ` holds microsecond precision, and an untruncated value fails round-trip equality assertions.

Returns `domain.ErrNotFound` when no row matches, mirroring `Update`'s `RowsAffected == 0` handling.

- [ ] **Step 1: Write the failing tests**

In `internal/store/postgres/agent_test.go`:
- `TestAgentStore_RecordHeartbeat` — records, reads back via `GetByName`, asserts `LastHeartbeatAt` is non-nil and equals the truncated input.
- `TestAgentStore_RecordHeartbeatUnknownAgentReturnsNotFound` — unknown name yields `domain.ErrNotFound` (Review Focus 3).
- `TestAgentStore_RecordHeartbeatIsScopedToTenant` — two tenants each with `security-agent`; recording for one leaves the other's `LastHeartbeatAt` nil (Review Focus 4).
- `TestAgentStore_GetByNameLeavesHeartbeatNilWhenNeverRecorded` — a freshly created agent reads back `LastHeartbeatAt == nil`, not a zero time.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/store/postgres/ -run TestAgentStore_RecordHeartbeat -v`
Expected: build failure — `RecordHeartbeat` undefined.

- [ ] **Step 3: Add `LastHeartbeatAt *time.Time` to `domain.Agent`**

In `internal/domain/agent.go`, with a doc comment explaining that nil means never heartbeated and is distinct from stale.

- [ ] **Step 4: Add the column to the GORM model and both mappers**

In `internal/store/postgres/models.go`: the `agentModel` field with its `gorm:"column:last_heartbeat_at"` tag, plus `agentFromDomain` and `agentModel.toDomain`. Cross-check the column name against `0002_heartbeats.sql`.

- [ ] **Step 5: Add `RecordHeartbeat` to the interface and implement it**

Interface method in `internal/store/store.go`; implementation on `*postgres.AgentStore` in `internal/store/postgres/agent.go`.

- [ ] **Step 6: Run the store tests**

Run: `go test ./internal/store/postgres/ -v`
Expected: PASS, all four new tests included.

- [ ] **Step 7: Commit**

```bash
git add internal/domain internal/store
git commit -m "feat: record agent heartbeats in the store"
```

---

### Task 4: Derive effective status

**Files:**
- Modify: `internal/domain/agent.go`
- Test: `internal/domain/agent_test.go`

**Interfaces:**
- Consumes: `domain.Agent.LastHeartbeatAt` (Task 3), `AgentStatus` constants.
- Produces: `func (a Agent) EffectiveStatus(cutoff time.Time) AgentStatus` and `const DefaultHeartbeatInterval = 30 * time.Second`, `const HeartbeatStaleAfter = 3 * DefaultHeartbeatInterval`.

`cutoff` is an absolute timestamp, not a duration and not a clock. The caller computes `now.Add(-HeartbeatStaleAfter)`. This mirrors the SQL predicate `last_heartbeat_at >= $cutoff`, so the Go derivation and the eventual SQL filter cannot drift, and it makes the function a pure function of its inputs — no clock to inject.

Rule: `nil` → `AgentUnknown`; `*LastHeartbeatAt` before `cutoff` → `AgentUnhealthy`; at or after → `AgentHealthy`. A future timestamp is therefore healthy, which is the correct handling of clock skew (Review Focus 1).

The `3 *` multiple needs a one-line comment: one missed heartbeat should not flip an agent, three missed means something is wrong.

- [ ] **Step 1: Write the failing boundary test**

In `internal/domain/agent_test.go`, `TestAgent_EffectiveStatus`, table-driven against a fixed `cutoff`:

| case | `LastHeartbeatAt` | want |
|---|---|---|
| never heartbeated | `nil` | `AgentUnknown` |
| one second after the cutoff | `cutoff.Add(time.Second)` | `AgentHealthy` |
| exactly at the cutoff | `cutoff` | `AgentHealthy` |
| one second before the cutoff | `cutoff.Add(-time.Second)` | `AgentUnhealthy` |
| long silent | `cutoff.Add(-24 * time.Hour)` | `AgentUnhealthy` |
| clock skew, future timestamp | `cutoff.Add(time.Hour)` | `AgentHealthy` |

Fixed timestamps only — no `time.Now()` anywhere in this test.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./internal/domain/ -run TestAgent_EffectiveStatus -v`
Expected: build failure — `EffectiveStatus` undefined.

- [ ] **Step 3: Implement `EffectiveStatus` and the two constants**

- [ ] **Step 4: Run the domain tests**

Run: `go test ./internal/domain/ -v`
Expected: PASS, no Docker needed.

- [ ] **Step 5: Commit**

```bash
git add internal/domain
git commit -m "feat: derive effective agent status from last heartbeat"
```

---

### Task 5: Staleness in the list query

**Files:**
- Modify: `internal/store/store.go`, `internal/store/postgres/agent.go`, `internal/api/agent.go`, `internal/api/dto.go`
- Test: `internal/store/postgres/agent_test.go`, `internal/api/agent_test.go`

**Interfaces:**
- Consumes: `EffectiveStatus` and `HeartbeatStaleAfter` (Task 4), `AgentFilter` (Phase 1).
- Produces: `AgentFilter.HealthyAfter time.Time` — the zero value means no staleness filtering. `agentResponse.LastHeartbeatAt *time.Time` with JSON tag `last_heartbeat_at,omitempty`.

The handler computes `HealthyAfter: time.Now().Add(-domain.HeartbeatStaleAfter)` and passes it down. The store's `?status=healthy` path compares against the column, not against the stored `status` value — the stored column can say `healthy` indefinitely once nothing sweeps it.

**The unfiltered list does not hide stale agents** (Success Criterion 3). `GET .../agents` with no `status` filter returns every agent, each reporting its computed status. The list is the operator's view; the filtered query is the router's.

`newAgentResponse` reports `Status` as `a.EffectiveStatus(cutoff)`, not `a.Status`, so a client never sees a stale `healthy`.

**Fake-store contract — read the `agentmesh-phase-dev` skill first.** `fakeAgentStore.List` must honour `HealthyAfter` exactly as the real store does, taking the pre-computed cutoff rather than calling a clock. `seedAgent` must default `LastHeartbeatAt` to a fresh value, or roughly ten existing tests begin returning zero agents the moment the handler passes a non-zero cutoff.

- [ ] **Step 1: Write the failing store test**

`TestAgentStore_ListFiltersStaleWhenStatusHealthy`: three agents — fresh heartbeat, heartbeat well before the cutoff, never heartbeated. With `Status: "healthy"` and a `HealthyAfter` cutoff, only the fresh one is returned. With no `Status` filter, all three are returned.

- [ ] **Step 2: Write the failing handler test**

`TestAgentHandler_ListOmitsStaleFromHealthyFilter` in `internal/api/agent_test.go`, driving `?status=healthy` through the mux. Include a case whose stored `Status` is `healthy` but whose heartbeat predates the cutoff — it must be excluded. That case is what proves the filter uses the timestamp rather than the column.

Add `seedAgentWithHeartbeat(f *fakeAgentStore, tenantID uuid.UUID, name string, heartbeatAt *time.Time, caps ...domain.Capability) domain.Agent` beside the existing `seedAgent`, rather than changing `seedAgent`'s signature at ten call sites.

- [ ] **Step 3: Run both to see them fail**

Run: `go test ./internal/store/postgres/ ./internal/api/ -run 'ListFiltersStale|ListOmitsStale' -v`
Expected: build failure — `HealthyAfter` undefined.

- [ ] **Step 4: Add `HealthyAfter` to `AgentFilter` and the SQL predicate**

- [ ] **Step 5: Wire the handler, the response mapper, and the fake**

Handler computes the cutoff; `newAgentResponse` takes it and reports the derived status; `fakeAgentStore.List` honours it; `seedAgent` defaults the heartbeat fresh.

- [ ] **Step 6: Run the full suite**

Run: `go test ./... -v`
Expected: PASS. Phase 1's existing agent handler tests must still pass unchanged — if they now return zero agents, Step 5's `seedAgent` default is missing.

- [ ] **Step 7: Commit**

```bash
git add internal/store internal/api
git commit -m "feat: filter stale agents from the healthy list query"
```

---

### Task 6: The Heartbeat gRPC server

**Files:**
- Create: `internal/grpcapi/server.go`, `internal/grpcapi/heartbeat.go`, `internal/grpcapi/heartbeat_test.go`
- Modify: `cmd/control-plane/main.go`, `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `store.TenantStore`, `store.AgentStore` (interfaces only — no gorm/pgx in this package either), the generated `AgentRegistrationServiceServer`, `domain.DefaultHeartbeatInterval`.
- Produces: `grpcapi.NewServer(cfg ServerConfig) *grpc.Server` where `ServerConfig` carries `Logger *slog.Logger`, `Tenants store.TenantStore`, `Agents store.AgentStore`. Config gains `GRPCAddr string` defaulting to `:9090`.

A second listener on its own port, not multiplexed — simpler, and it leaves the HTTP health-probe story untouched.

Resolve `tenant_slug` to a tenant, then call `RecordHeartbeat`. An unknown tenant or agent maps to `codes.NotFound`; a validation failure to `codes.InvalidArgument`. Never create a row: agents are operator-declared, so a heartbeat from something unregistered is a misconfiguration worth surfacing.

The response sets `next_heartbeat_after` from `domain.DefaultHeartbeatInterval`, so the control plane owns the cadence and independently-deployed agents cannot drift from the staleness threshold.

The announced `capabilities` are **logged, not written to the database.** Writing them would mean deciding what to do when a heartbeat disagrees with the registered set, which is Phase 6's drift-detection question. Logging proves the field arrives intact and costs no schema. Log them as a structured field on the heartbeat log line, never as a bare count — Phase 6 will want the contents.

Graceful shutdown: `main.go` must stop the gRPC server as part of the existing sequence, and the DB pool must still close only after *both* servers have stopped. Phase 1's ordering — pool closed after the HTTP server fully stops — is the pattern to extend, not to race.

- [ ] **Step 1: Write the failing heartbeat tests**

In `internal/grpcapi/heartbeat_test.go`, against the fake stores (reuse the package-local fakes pattern; the skill's contract applies):
- a valid heartbeat returns a response whose `next_heartbeat_after` equals `DefaultHeartbeatInterval` and calls `RecordHeartbeat` once;
- an unknown tenant slug yields `codes.NotFound`;
- an unknown agent name yields `codes.NotFound`;
- a heartbeat never creates a row (assert the store's create path was not called);
- the announced capabilities reach the log line (capture the logger into a buffer, as Phase 1's middleware tests do, and assert the names appear) — this is the only test that proves the proto field is wired through rather than silently dropped.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/grpcapi/ -v`
Expected: build failure — package does not exist.

- [ ] **Step 3: Implement the server and service**

- [ ] **Step 4: Add `GRPCAddr` to config with its validation and default**

Repair the config tests the new required-field validation breaks, as Phase 1's Task 9 did — do not weaken existing assertions to pass.

- [ ] **Step 5: Wire both servers into `main.go` with shared graceful shutdown**

- [ ] **Step 6: Verify layering and run the suite**

Run: `go list -deps ./internal/api | grep -E 'gorm|pgx'` (expect empty), then `go test ./... -v`
Expected: empty, then PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/grpcapi internal/config cmd/control-plane
git commit -m "feat: serve the heartbeat gRPC service alongside REST"
```

---

### Task 7: The example agent

**Files:**
- Create: `cmd/example-agent/main.go`, `cmd/example-agent/main_test.go`
- Modify: `Makefile`, `.env.example`

**Interfaces:**
- Consumes: the generated `AgentServiceServer` and `AgentRegistrationServiceClient`, `domain` constants.
- Produces: a binary configurable by environment variables, runnable multiple times with different settings.

Configuration, following Phase 1's `internal/config` pattern (read, validate, fail fast with a clear message): control-plane gRPC address, tenant slug, agent name, listen address, and a capability list.

Serves `AgentService` deterministically — same input, same output — so Phase 3's router tests have something predictable to route to.

Heartbeats on the interval the *response* advertises, not a local constant.

**Survives a control plane that is down or restarting** (Review Focus 5): a failed heartbeat is logged and retried on the next tick, never fatal. An agent that exits because the control plane blinked cannot demonstrate the recovery Success Criterion 2 requires.

`make` targets to run one or more example agents. The Phase 1 review found the DSN literal duplicated across three targets — fold the shared value into one variable rather than adding a fourth copy.

- [ ] **Step 1: Write the failing retry test**

`TestHeartbeatLoop_SurvivesControlPlaneFailure`: a heartbeat function that fails on the first N calls and then succeeds; assert the loop keeps calling and does not return an error. Use a channel or a counter, not sleeps.

- [ ] **Step 2: Run it to see it fail**

Run: `go test ./cmd/example-agent/ -v`
Expected: build failure — package does not exist.

- [ ] **Step 3: Implement the agent: config, `AgentService` server, heartbeat loop**

- [ ] **Step 4: Add the `make` targets and `.env.example` entries**

- [ ] **Step 5: Run the suite and the lint gate**

Run: `go test ./... && /tmp/golangci-v2-install/golangci-lint run ./...`
Expected: PASS and `0 issues.`

- [ ] **Step 6: Commit**

```bash
git add cmd/example-agent Makefile .env.example
git commit -m "feat: add example agent that serves capabilities and heartbeats"
```

---

### Task 8: End-to-end verification and documentation

**Files:**
- Create: `docs/adr/0009-agents-push-heartbeats.md`, `docs/adr/0010-computed-staleness-over-background-sweeper.md`
- Modify: `docs/learning-guide.md`, `docs/architecture.md`, `README.md`

**Interfaces:**
- Consumes: everything above.
- Produces: no code.

- [ ] **Step 1: Run the stack end to end and capture real output**

`make dev`, `make migrate`, `make seed`, start the control plane, start an example agent. Observe `unknown` → `healthy` with no operator action. Kill the agent; observe `unhealthy` within the documented interval. Restart it; observe `healthy` again. Confirm `?capability=X` returns only healthy matches while the unfiltered list still shows the stale agent.

**Tear the Docker stack down the moment this step finishes** — `make compose-down`, then verify with `docker ps -a --filter name=agentmesh`. A killed process cannot run its own cleanup, and Phase 1 left a credentialed Postgres on the LAN for eight hours exactly this way.

- [ ] **Step 2: Write ADR 0009 (push vs poll)**

Record what polling would have bought — endpoint validation exercised at registration time, which Phase 1's unvalidated-`Endpoint` gap makes concretely valuable — and why push won anyway. An ADR recording only the upside is not worth writing; ADR 0008 was strengthened on exactly this point during Phase 1 review.

- [ ] **Step 3: Write ADR 0010 (computed staleness vs sweeper)**

Include the pre-computed-cutoff decision and why the store takes a timestamp rather than a clock. This is the decision most likely to be revisited, so the reasoning must be findable.

- [ ] **Step 4: Add the Phase 2 learning-guide section**

Follow the established nine-heading format. Candidate topics: push vs poll and what each assumes about network topology; derived state vs stored state; why a unary heartbeat beats a stream for liveness; pure functions over injected clocks.

Carry forward the known-gaps list, including `Endpoint` still unvalidated as a gRPC target and still undialed.

- [ ] **Step 5: Update `architecture.md` and `README.md`**

The agent component becomes real rather than "Phase 2". Document the new port and `make` targets. Extend the Security Model: **the heartbeat endpoint is also unauthenticated**, so any process that can reach the control plane can claim to be any registered agent. Phase 5's to fix; write it plainly rather than leaving it implied, exactly as Phase 1 did for the REST surface.

- [ ] **Step 6: Run every Quick Start command the README now contains, in order**

A README that does not run is worse than none.

- [ ] **Step 7: Commit**

```bash
git add docs README.md
git commit -m "docs: add Phase 2 ADRs, learning guide, and README updates"
```

---

### Task 9: Spec-conformance pass

**Files:** none — this task reads.

**Interfaces:** none.

Phase 1's worst defect survived eight green review gates: the spec's validation table requires `Agent.Endpoint` to "parse as a gRPC target" and the code checks only non-empty and length. Nobody caught it because every task reviewer read its own brief's Produces list, never the spec's tables.

Task gates verify that briefs were implemented. They cannot verify that briefs covered the spec. This pass closes that hole.

- [ ] **Step 1: Walk the spec against the code**

Read `docs/superpowers/specs/2026-10-17-agentmesh-phase2-design.md` top to bottom with the shipped code open. For every sentence stating a rule, a value, or a required behaviour, find the code that implements it and the test that proves it. Work from the spec, not from this plan.

- [ ] **Step 2: Check each Success Criterion individually**

All six, including the unfiltered-list clause in Criterion 3 and the real-container requirement in Criterion 5.

- [ ] **Step 3: Record findings**

Anything unimplemented is a finding even if every task gate passed. Fix it or record it in the known-gaps list with the phase that owns it — do not leave it undocumented.

- [ ] **Step 4: Final whole-branch verification**

Run: `go build ./... && go vet ./... && go test ./... && /tmp/golangci-v2-install/golangci-lint run ./... && go list -deps ./internal/api | grep -E 'gorm|pgx'`
Expected: all pass, `0 issues.`, and the last command empty.
