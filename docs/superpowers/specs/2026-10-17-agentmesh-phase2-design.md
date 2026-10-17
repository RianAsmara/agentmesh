# AgentMesh — Phase 2 Design (gRPC Agent Contract, Example Agents, Heartbeats)

Date: 2026-10-17
Status: draft, pending approval
Predecessor: `2026-10-14-agentmesh-phase1-design.md` (complete, merged at `b0f209e`)

## Purpose

Phase 1 shipped a registry whose health column nobody writes. `agents.status`
is whatever an operator typed at create time, it defaults to `unknown`, and
no code path ever changes it. Phase 3's router is supposed to pick healthy
agents; today "healthy" means "an operator claimed so, once, possibly weeks
ago."

Phase 2 makes health mean something. Agents report in on an interval over
gRPC, and an agent that goes quiet stops being considered healthy. That is
the whole point of the phase: `status` becomes a fact the system observed
rather than a string it was told.

Three steps, each needing the previous one:

1. **The gRPC agent contract.** A service definition agents implement and a
   heartbeat RPC they call. Phase 0 scaffolded `common.proto` with
   `RequestMetadata` and `ErrorDetail`; this is the first real service.
2. **Example agents.** Real processes that speak that contract. The
   architecture document already anticipates them as "deterministic mock
   capability providers." They exist so there is something concrete to
   heartbeat from, and so Phase 3 has real endpoints to dial.
3. **Heartbeats.** Agents push; the control plane records arrivals.

## Success Criteria

1. `make dev && make migrate && make seed` then starting the control plane
   and one or more example agents yields agents that transition `unknown` →
   `healthy` on their own, with no operator action.
2. Killing an example agent causes its status to become `unhealthy` within a
   bounded, documented interval, observable through
   `GET /v1/tenants/{slug}/agents`. Restarting it returns the agent to
   `healthy`.
3. `GET /v1/tenants/{slug}/agents?capability=X` returns only agents that are
   both capability-matched and currently healthy — the query Phase 3's router
   actually needs.
   **The unfiltered list does not hide stale agents.** `GET .../agents` with no
   status filter returns every agent, each reporting its true computed status.
   Hiding a stale agent there would remove precisely the information an
   operator needs in order to notice something is down — the list is the
   operator's view, while the filtered query is the router's. Filtering is
   opt-in via `?status=` or `?capability=`; absence of a filter is not an
   implicit request for healthy-only.
4. An agent heartbeating for a tenant or name it does not own is rejected,
   and the rejection is tested.
5. The heartbeat path is exercised against a real PostgreSQL container, not
   only against fakes.
6. `docs/learning-guide.md` gains a Phase 2 section following the Phase 0/1
   structure, and `docs/architecture.md` plus `README.md` reflect the new
   component.

## Decisions Already Fixed by Prior ADRs

- PostgreSQL is the single source of truth (ADR 0002). Heartbeat state lives
  there; no in-memory-only health.
- REST/JSON at `/v1/...` for management; gRPC for data-plane and
  agent-facing communication (ADR 0007). Heartbeats are gRPC, not REST.
- Server-streaming gRPC is the chosen shape for configuration distribution
  (ADR 0004) — that is Phase 4's concern, but the agent contract defined here
  must not foreclose it.
- Authorization sits behind an `authorization.Decider` interface, RBAC first
  (ADR 0006). Phase 2 implements none of it.

## Decisions Made in This Phase

These were settled in conversation with the project owner and are binding.

### Agents push; the control plane never dials out

The control plane does not poll agents. An agent calls `Heartbeat` on the
control plane on an interval; the control plane records the arrival and
nothing more.

Chosen because agents may live behind NAT — which matters if agents are ever
customer-deployed rather than co-located — and because the control plane's
work then does not scale with agent count. It is told about health rather
than discovering it.

The cost, accepted knowingly: the stored `Endpoint` remains undialed by
anything until Phase 3. Phase 1's open gap, that `Endpoint` is validated as
non-empty and ≤512 characters but *not* as a parseable gRPC target
(spec Section 1's validation table requires the latter), therefore stays
open through Phase 2. Polling would have forced it closed now. This was
raised explicitly and the push model was still chosen; the gap remains
Phase 3's to close, and remains listed in the learning guide's known-gaps
section.

### Staleness is computed on read, not by a background sweeper

**This is the design author's call, not the owner's.** It was made because
the owner delegated the remaining decisions; it should be the first thing
reviewed, and it is cheap to reverse before implementation begins.

An agent's effective status is derived when someone asks, by comparing
`last_heartbeat_at` against a staleness threshold. There is no goroutine
sweeping the table flipping rows to `unhealthy`.

Reasoning:

- A sweeper is a second writer to `agents`, racing the heartbeat path. Two
  writers to one row demands either locking or an accepted lost-update, and
  neither earns its complexity here.
- A sweeper's flip is a side effect that happens whether or not anyone is
  looking. Computed staleness cannot drift from the data it is derived from,
  because it *is* the data.
- A sweeper's correctness depends on its interval; computed staleness is
  correct the instant it is read.
- Restart behaviour is free: a control plane that was down for an hour does
  not need to catch up on a backlog of flips. The timestamps still say what
  they said.

The cost: every read that cares about health does a timestamp comparison, and
the threshold becomes part of the read path rather than a config knob on a
background job. Accepted — it is one comparison against an indexed column.

A follow-on consequence worth stating plainly: because status is computed,
the stored `status` column alone no longer tells the truth. Section 2 resolves
that.

### Heartbeats carry capability state, not just liveness

A heartbeat is the agent asserting what it currently is, not merely that it
exists. This keeps the door open for an agent whose capabilities change at
runtime without re-registration, and it means a heartbeat is useful to
Phase 3 beyond a liveness bit.

Phase 2 does *not* implement capability reconciliation from heartbeats — that
is drift detection, which the roadmap places at Phase 6. The contract carries
the field; Phase 2 records it and acts only on liveness.

## Section 1 — The gRPC Agent Contract

New file: `api/proto/agentmesh/v1/agent.proto`, alongside the existing
`common.proto`. Generated into `api/proto/gen/go/agentmesh/v1/` by the
existing `buf.gen.yaml` (protoc-gen-go + protoc-gen-go-grpc,
`paths=source_relative`). `buf lint` uses the STANDARD ruleset and must stay
clean; `buf breaking` is configured against `FILE` and this is a new file, so
there is nothing to break yet.

Two distinct directions must not be conflated, and the proto should make the
distinction obvious to a reader:

**Agent-implemented** (`AgentService`) — what Phase 3's router will call on
an agent. Phase 2 defines it and the example agents implement it, so the
contract is proven by a real implementation rather than asserted. Phase 2
itself does not call it. At minimum it needs the capability-invocation RPC
Phase 3 depends on; the design intentionally leaves the invocation payload
shape to the implementer, constrained only by needing to carry
`RequestMetadata` and to return `ErrorDetail` on failure.

**Control-plane-implemented** (`AgentRegistrationService`, or a name the
implementer prefers that distinguishes direction) — what agents call. This is
where `Heartbeat` lives.

`Heartbeat` is a unary RPC, not streaming. A stream would conflate connection
liveness with agent liveness: a half-open TCP connection reads as healthy
while the process behind it is wedged. A unary call per interval produces an
unambiguous timestamp. ADR 0004's streaming decision is about configuration
distribution and does not apply here.

The request must identify the agent by the pair the registry already treats
as identity — tenant slug and agent name, per Phase 1's
`UNIQUE (tenant_id, name)` — not by UUID. Phase 1's spec fixed name-based
addressing ("greppable in logs, typeable in curl") and an agent process
should not need to learn a UUID to report in. It carries currently-announced
capabilities, and embeds `RequestMetadata`.

The response must tell the agent how long to wait before the next heartbeat,
rather than letting each agent hardcode an interval. The control plane owns
the cadence, so the staleness threshold and the heartbeat interval cannot
drift apart across independently-deployed agents.

An unknown tenant/agent pair is an error, not an implicit registration.
Agents are operator-declared in this system; a heartbeat from something
unregistered is a misconfiguration worth surfacing, not an invitation to
create a row.

## Section 2 — Schema and Domain Changes

Migration `0002_heartbeats.sql`. Phase 1's migration runner sorts
zero-padded filenames, and the Task 2 review noted no ordering test exists
above a single file — whoever adds this second migration must add that test.

`agents` gains a nullable `last_heartbeat_at TIMESTAMPTZ`. Nullable is
correct and load-bearing: `NULL` means "never heard from," which is exactly
`unknown`, and is distinct from "heard from, long ago," which is
`unhealthy`. Collapsing those two into one value would lose the distinction
Phase 1 deliberately introduced `unknown` to express.

Because status is computed (Section 1's second decision), the existing
`status` column and the derived value must not be allowed to disagree
silently. The implementer resolves this one of two ways, and must state
which and why in the ADR:

- Keep `status` as the operator-settable field it is today and derive the
  effective status on read, leaving the column as declared intent.
- Treat the column as a cache of the computed value, updated on heartbeat.

The first is more honest and avoids a second writer; the second is faster to
query. Either is defensible; what is not defensible is shipping both
meanings and letting readers guess.

The staleness threshold must be a named constant or configuration value with
a documented relationship to the heartbeat interval the response advertises —
not a literal buried in a query. A threshold of exactly one interval would
flap on a single delayed heartbeat; some multiple is wanted, and the chosen
multiple needs a one-line justification where it is defined.

Index `last_heartbeat_at` if the capability query filters on it. Phase 1's
capability filter uses a correlated `EXISTS` subquery; adding a staleness
predicate to that query is the point where an index earns itself.

The domain layer keeps owning validation. `AgentStatus` already has
`unknown`, `healthy`, `unhealthy` and a `Valid()` method — Phase 2 adds no
new status value.

## Section 3 — Example Agents

New binary under `cmd/`, one that can be run multiple times with different
configuration rather than several near-identical binaries. The architecture
document calls these "deterministic mock capability providers" — determinism
is the requirement: given the same input they return the same output, so
Phase 3's router tests have something predictable to route to.

Each example agent:

- Serves `AgentService` on a configurable address, so a real endpoint exists
  for Phase 3 to dial and for the contract to be proven by implementation.
- Heartbeats to the control plane on the interval the control plane
  advertises, honouring the response rather than a local constant.
- Announces a configurable capability set, so the capability filter can be
  exercised across more than one agent.
- Survives a control plane that is down or restarting: heartbeat failures are
  logged and retried on the next tick, never fatal. An agent that exits
  because the control plane blinked is worse than useless for demonstrating
  recovery, which Success Criterion 2 requires.

Configuration follows Phase 1's pattern — environment variables read through
a small config loader, validated at startup, failing fast with a clear
message. Phase 1's `internal/config` is the model, including its new upper
bounds on numeric values.

`make` targets to run one or more example agents alongside the control plane,
following the existing `dev`/`seed`/`run-control-plane` pattern. The Phase 1
review found the DSN literal duplicated across three targets; Phase 2 should
not add a fourth copy.

## Section 4 — Serving gRPC Alongside REST

The control plane currently serves only HTTP via `api.NewRouter`. It must now
also serve gRPC, and the implementer decides between a second listener on its
own port or multiplexing one port — stating the choice and its reasoning.
A separate port is the simpler default and keeps the health-probe story
unchanged.

Constraints carried from Phase 1, all verified there and not to be regressed:

- `internal/api` imports no `gorm` or `pgx`. The gRPC service layer holds
  store interfaces, exactly as the HTTP handlers do. Whatever package the
  gRPC server lives in inherits this rule.
- Graceful shutdown must cover both servers. Phase 1's `main.go` closes the
  DB pool only after the HTTP server has fully stopped; the gRPC server joins
  that ordering rather than racing it.
- `/readyz` reflects real database reachability and `/healthz` does not touch
  the database. Adding gRPC must not change either.
- Phase 1's middleware ordering — `WithRecovery` innermost — exists because
  of a bug that cost a full fix round. If gRPC gets interceptors, the
  equivalent ordering question arises independently; the learning guide's
  topic 8 explains the reasoning to reapply.

## Section 5 — Testing Strategy

Following Phase 1: domain and pure logic tested without Docker, store and
heartbeat-persistence paths tested against a real `postgres:17-alpine`
container via the existing shared-container harness in
`internal/store/postgres/testutil_test.go`.

Specific coverage this phase must have, each chosen because its absence would
hide a real defect:

- **Staleness boundary.** An agent exactly at the threshold, just inside, and
  just outside. Off-by-one here is the difference between flapping and never
  flipping.
- **`NULL` versus stale.** A never-heartbeated agent reads `unknown`; a
  long-silent one reads `unhealthy`. The distinction the nullable column
  exists for must be asserted.
- **Heartbeat for an unregistered agent** is rejected, and for an agent in
  another tenant is rejected. Phase 1's cross-tenant isolation has tests;
  this is the same property on a new path.
- **The capability query with staleness applied** returns a healthy matching
  agent and omits a stale one. This is Success Criterion 3 and the query
  Phase 3 depends on.
- **Recovery.** An agent that went stale and heartbeats again returns to
  healthy. Success Criterion 2's second half.
- **End-to-end**, run manually and documented: control plane plus example
  agents, agent killed, status observed changing, agent restarted, status
  observed recovering.

A warning from Phase 1, worth heeding precisely because it recurred five
times: every fake store that diverged from the real store's contract hid a
defect, and in four of those cases the brief's own prescribed fake could not
satisfy the brief's own test. Any fake written for the heartbeat path must
mirror the real store's contract — specifically, it must stamp timestamps the
way the real store does, because a fake that leaves `last_heartbeat_at` zero
makes every staleness test meaningless while still passing.

Time must be injectable. Tests cannot wait out a real staleness threshold,
and a test that sleeps is a test that flakes. Whether that is a clock
interface, a function field, or an explicit `now` parameter is the
implementer's call, but hardcoded `time.Now()` in the staleness path is a
defect, not a style preference.

**The store takes a pre-computed cutoff, not a duration or a clock.** The
filter carries an absolute timestamp — the caller computes `now - threshold`
and the store compares against it, mirroring the SQL predicate
`last_heartbeat_at >= $cutoff`. Two reasons. The real store will receive a
bound parameter, so a fake that calls its own clock diverges from the real
contract on exactly the field under test — the five-time Phase 1 failure. And
a store that is a pure function of its inputs needs no clock injection at all;
determinism comes free rather than by wiring.

This surfaced from a trial implementation that got the cutoff design right
while getting the nullable timestamp wrong, which is worth recording: the two
decisions are independent.

A warning drawn from the same trial. An implementation without injectable time
reasoned itself *out* of boundary testing — "an exact boundary test would be
flaky, because the test and the handler both call `time.Now()`
independently" — and substituted wide margins (half the window, four times the
window). The diagnosis was correct and the fix was wrong: the flakiness is the
testability defect talking. Weakening coverage to accommodate an untestable
design is how off-by-one at the threshold ships. Boundary cases are required;
if they look flaky, fix the clock, not the test.

## Section 6 — Documentation

- **ADR** for the push-versus-poll decision, recording what polling would
  have bought — endpoint validation exercised at registration time, which
  Phase 1's gap makes concretely valuable — and why push won anyway. Phase 1's
  spec set the standard that an ADR recording only the upside is not worth
  writing; ADR 0008 was strengthened on exactly this point during Phase 1
  review.
- **A second ADR** for the computed-versus-swept staleness decision, or a
  clearly-marked section of the first. This is the decision most likely to be
  revisited, so the reasoning needs to be findable.
- **`docs/learning-guide.md`** gains a Phase 2 section in the established
  nine-heading format. Candidate topics: push versus poll and what each
  assumes about network topology; derived state versus stored state; why a
  unary heartbeat beats a stream for liveness; injectable time.
- **`docs/architecture.md`** and **`README.md`** updated — the agent
  component becomes real rather than "Phase 2," the new port and `make`
  targets documented, and the README's Security Model extended: the heartbeat
  endpoint is **also unauthenticated**, so any process that can reach the
  control plane can claim to be any registered agent. That is Phase 5's to
  fix and must be written down plainly rather than implied, exactly as Phase 1
  did for the REST surface.
- **Known gaps** carried forward, including `Endpoint` still unvalidated as a
  gRPC target and still undialed.

## Out of Scope for Phase 2

- Routing or invoking agents (Phase 3). `AgentService` is defined and
  implemented by example agents; nothing calls it.
- Configuration distribution (Phase 4).
- Authentication and authorization, including authenticating heartbeats
  (Phase 5).
- Capability drift reconciliation from heartbeat contents (Phase 6).
- Retries, circuit breaking, rate limiting on the heartbeat path (Phase 7).
- Metrics and tracing (Phase 8). The heartbeat path is an obvious future
  metric source; Phase 2 adds structured logs only.
- mTLS between agents and control plane (Phase 9).
- Web dashboard (Phase 10). Raised during this phase's discussion and
  deliberately deferred: with heartbeats landing now and the router arriving
  in Phase 3, a dashboard built before both would display a static table and
  be rebuilt twice.
- Closing Phase 1's `Endpoint` gRPC-target validation gap (Phase 3, which is
  the first phase to dial it).

## Task Breakdown

Sequenced so each task ends on a tree where `go build ./...` and
`go test ./...` both pass — the invariant Phase 1's per-task review gate
depended on.

1. `agent.proto`: both services, generated code, `buf lint` clean.
2. Migration `0002_heartbeats.sql` plus the multi-migration ordering test
   Phase 1's review flagged as missing.
3. Store layer: persist a heartbeat, query with staleness, injectable time.
4. Domain/derivation layer: effective status from `last_heartbeat_at`, with
   boundary tests.
5. gRPC server for `Heartbeat`, wired into the control plane with graceful
   shutdown covering both servers.
6. Capability query gains the staleness predicate; Phase 1's existing
   handler tests must still pass.
7. Example agent binary: serves `AgentService`, heartbeats, configurable.
8. `make` targets, local development wiring, end-to-end manual verification.
9. Documentation: two ADRs, learning guide, architecture, README.

Phase 1 ran ten tasks with a code review gate between each, and that gate
caught two Important findings the per-task tests missed. The same gate is
assumed here.

One process change from Phase 1's retrospective: per-task reviewers read
their own task brief and never the spec, so a validation rule in the spec
went unimplemented through eight green gates. Phase 2 needs an explicit
spec-conformance pass that reads *this document* against the shipped code
before the phase is declared complete.
