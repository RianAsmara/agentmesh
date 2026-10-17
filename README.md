# AgentMesh

A secure, multi-tenant control plane and Go/gRPC data plane for routing
communication between autonomous AI agents and internal tools.

## Project Status

**Phase 1 of 12 complete: agent registry and REST API.**

What exists right now: a tenant/agent/capability registry exposed over a
REST API (`/v1/tenants/...`), backed by PostgreSQL. Tenants and agents are
created, read, listed (with cursor pagination), patched, and deleted;
agents are scoped to a tenant and carry a list of capabilities used for
filtering (`?capability=...`). Schema changes are applied by versioned SQL
migrations (`internal/migrate`, embedded and run by `make migrate`), not by
ORM auto-migration. A seed command (`make seed`) loads a demo tenant
(`acme`) with three agents. `/healthz` and `/readyz` report liveness and
readiness separately, the latter by checking the database. Nothing else
described in this README is implemented yet — each section below says what
phase delivers it.

See [`docs/learning-guide.md`](docs/learning-guide.md) for the full,
phase-by-phase build log and [`docs/architecture.md`](docs/architecture.md)
for the target architecture diagram.

## Problem Statement

Autonomous AI agents increasingly need to call each other and internal
tools — a planning agent handing work to a backend-implementation agent,
which hands off to a security-review agent, for example. Doing this safely
across multiple tenants requires the same problems any service mesh solves:
capability-based routing, authorization, tenant isolation, configuration
distribution, drift detection, and observability — applied to agent
traffic instead of generic microservice traffic.

## Architecture

AgentMesh separates a **control plane** (tenant/agent/route management,
desired configuration state — Go, PostgreSQL, REST + internal gRPC) from a
**data plane** (a native Go router that enforces that configuration on the
actual invocation path — no Envoy dependency for v1; see
[ADR 0001](docs/adr/0001-native-go-router-instead-of-envoy.md)). Full
diagram and component table: [`docs/architecture.md`](docs/architecture.md).

## Quick Start

Requires Go 1.26+ and Docker (for local PostgreSQL).

```bash
git clone <this-repo>
cd agentmesh
make dev        # start PostgreSQL in Docker
make migrate    # apply the schema
make seed       # load the acme demo tenant and three agents
make run-control-plane

# in another terminal:
curl -s http://localhost:8080/readyz
curl -s http://localhost:8080/v1/tenants
curl -s "http://localhost:8080/v1/tenants/acme/agents?capability=security-review"
```

The last command returns `security-agent`, the only one of the three seed
agents that announces a `security-review` capability.

`make dev` starts PostgreSQL bound to `127.0.0.1:5432` (loopback only —
never exposed to the LAN) with a fixed development password defined in
`docker-compose.yml`. `make seed` is idempotent: running it again reuses
the existing `acme` tenant and skips agents that already exist, instead of
erroring or duplicating rows. When you're done, tear the stack down with
`make compose-down`.

## API

Ten `/v1` endpoints, all under `/v1/tenants`:

| Method | Path | Description |
|---|---|---|
| `POST` | `/v1/tenants` | Create a tenant |
| `GET` | `/v1/tenants` | List tenants (cursor-paginated) |
| `GET` | `/v1/tenants/{slug}` | Get a tenant |
| `PATCH` | `/v1/tenants/{slug}` | Update a tenant's `name` and/or `status` |
| `DELETE` | `/v1/tenants/{slug}` | Delete a tenant (cascades to its agents) |
| `POST` | `/v1/tenants/{slug}/agents` | Register an agent under a tenant |
| `GET` | `/v1/tenants/{slug}/agents` | List a tenant's agents (filter by `?capability=` and/or `?status=`, cursor-paginated) |
| `GET` | `/v1/tenants/{slug}/agents/{name}` | Get an agent |
| `PATCH` | `/v1/tenants/{slug}/agents/{name}` | Update an agent's `endpoint` and/or `status` |
| `DELETE` | `/v1/tenants/{slug}/agents/{name}` | Delete an agent (cascades to its capabilities) |

Plus `GET /healthz` (liveness, ignores the database) and `GET /readyz`
(readiness, checks the database).

Every failure uses the same error envelope:

```json
{
  "error": {
    "code": "invalid",
    "message": "field \"slug\" is invalid: must be between 3 and 63 characters",
    "details": [{"field": "slug", "issue": "must be between 3 and 63 characters"}]
  }
}
```

`code` is one of `invalid` (400), `not_found` (404), `already_exists`
(409), or `internal` (500); `details` is present only for validation
failures. The `code` values are part of the API contract and don't change
across releases — `message` may.

## Example Workflow

Not yet implemented. The target demo scenario — a tenant `acme` with
`planner-agent`, `backend-agent`, and `security-agent` registered, and a
`security-review` request routed end-to-end — is described in the original
project brief and will be runnable starting once Phase 3 (basic router)
ships.

## Security Model

**Every endpoint is currently unauthenticated.** `/v1/*`, `/healthz`, and
`/readyz` all accept any caller — there is no API key, token, or session
of any kind yet. This is a deliberate, written-down decision for Phase 1,
not an oversight: authentication and authorization arrive in Phase 5.

The local development Postgres started by `make dev` is bound to
`127.0.0.1:5432` (loopback only, never exposed to the LAN) and ships with a
well-known development password (`agentmesh`/`agentmesh`, see
`docker-compose.yml`). That password must never be reused outside local
development.

See "Known gaps carried into later phases" in
[`docs/learning-guide.md`](docs/learning-guide.md) for related gaps —
in particular, the `suspended` value of `TenantStatus`
(`domain.TenantSuspended`) has no enforced behavioural meaning yet, which
matters once Phase 5 builds authorization on top of it. A full threat
model, trust boundaries, and mitigations will be
documented in `docs/security-model.md`, created in Phase 9 (mTLS) and
Phase 5 (multi-tenancy/authorization).

## Consistency Model

Not yet implemented in code, but the design decision is recorded now:
AgentMesh uses eventual consistency between desired and applied router
configuration, with explicit drift detection rather than an attempt at
strong consistency. See
[ADR 0005](docs/adr/0005-eventual-consistency-for-router-configuration.md).
Full write-up lands in `docs/consistency-model.md` in Phase 6.

## Observability

Not yet implemented. Structured JSON logging exists today
(`internal/observability`); metrics and distributed tracing arrive in
Phase 8.

## Performance Testing

Not yet implemented. Benchmarks, load tests, and a profiling workflow
arrive in Phase 12.

## Roadmap

| Phase | Delivers |
|---|---|
| 0 ✅ | Repository bootstrap |
| 1 ✅ (this phase) | Agent registry, basic control plane, REST API |
| 2 | gRPC agent contract, example agents |
| 3 | Basic router |
| 4 | Configuration distribution |
| 5 | Multi-tenancy and authorization |
| 6 | Drift detection and reconciliation |
| 7 | Reliability (retries, circuit breakers, rate limiting) |
| 8 | Observability (tracing, metrics, dashboards) |
| 9 | mTLS |
| 10 | Web dashboard |
| 11 | Kubernetes deployment |
| 12 | Performance and portfolio polish |

## Trade-offs

Key architectural trade-offs are recorded as ADRs in
[`docs/adr/`](docs/adr/) as they're made, rather than summarized here where
they'd go stale. Start with ADR 0001 (native Go router vs. Envoy) and
ADR 0005 (eventual consistency for router configuration).

## License

MIT — see [`LICENSE`](LICENSE).
