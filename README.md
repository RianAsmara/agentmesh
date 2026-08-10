# AgentMesh

A secure, multi-tenant control plane and Go/gRPC data plane for routing
communication between autonomous AI agents and internal tools.

## Project Status

**Phase 0 of 12 complete: repository bootstrap.**

What exists right now: a Go module, a control-plane service exposing only a
`/healthz` endpoint, protobuf tooling configuration (no services defined
yet), lint configuration, a Makefile, initial architecture decision
records, and documentation skeletons. Nothing else described in this README
is implemented yet — each section below says what phase delivers it.

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

Requires Go 1.26+.

```bash
git clone <this-repo>
cd agentmesh
make build
make run-control-plane
# in another terminal:
curl -s http://localhost:8080/healthz
# => {"status":"ok"}
```

## Example Workflow

Not yet implemented. The target demo scenario — a tenant `acme` with
`planner-agent`, `backend-agent`, and `security-agent` registered, and a
`security-review` request routed end-to-end — is described in the original
project brief and will be runnable starting once Phase 3 (basic router)
ships.

## Security Model

Not yet implemented. Threat model, trust boundaries, and mitigations will
be documented in `docs/security-model.md`, created in Phase 9 (mTLS) and
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
| 0 (this phase) | Repository bootstrap |
| 1 | Agent registry, basic control plane, REST API |
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
