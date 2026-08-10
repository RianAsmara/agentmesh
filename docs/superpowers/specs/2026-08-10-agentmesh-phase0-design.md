# AgentMesh — Phase 0 Design (Bootstrap)

Status: Approved
Date: 2026-08-10

## Context

AgentMesh is a portfolio-grade, multi-tenant control plane and Go/gRPC data
plane for routing communication between autonomous AI agents and internal
tools. The full project is specified across 13 phases (see the project brief
in the session that produced this spec). This document covers **Phase 0
only**: bootstrap the repository so every later phase has a working
foundation to build on. No application features beyond a minimal health
endpoint are in scope.

## Decisions

1. **Go module path:** `agentmesh` (local-only, no VCS host prefix). Chosen
   by the project owner over a `github.com/<user>/agentmesh` path since the
   repo isn't tied to a specific GitHub account yet.
2. **Protobuf tooling:** [buf](https://buf.build) instead of raw `protoc`
   invocations. `buf` is not installed in the dev environment; the `make
   generate` target installs it via `go install` on first run rather than
   silently shelling out to a package manager.
3. **License:** MIT.
4. **Version control:** `git init` a local repository and make one commit at
   the end of Phase 0. Nothing is pushed to a remote.
5. **Scaffolding discipline:** only create directories/packages that have
   real content in this phase. `cmd/control-plane` gets a working health
   endpoint; other `cmd/*` targets, `migrations/`, `deploy/compose/`, `web/`
   etc. are deferred to the phases that actually populate them, per the
   project rule against creating packages purely to match a reference tree.

## Deliverables

- `go.mod` — module `agentmesh`, Go 1.26.
- `cmd/control-plane/main.go` — `/healthz` HTTP endpoint (stdlib
  `net/http`), graceful shutdown, structured startup logging.
- `internal/observability/logger.go` — `slog` setup shared by services.
- `internal/config/config.go` — minimal env-based config loader with
  startup validation.
- `api/proto/agentmesh/v1/common.proto` + `buf.yaml` + `buf.gen.yaml` —
  protobuf tooling scaffold (no service definitions yet — those arrive in
  Phase 2).
- `.golangci.yml` — lint configuration.
- `Makefile` — real targets: `build`, `test`, `lint`, `generate`,
  `run-control-plane`. Stub targets for functionality that doesn't exist
  yet (`migrate`, `seed`, `dev`, `run-router`, `run-agents`, `load-test`,
  `profile`, `compose-down`), each printing which phase implements it.
- `docs/adr/0000-template.md` plus 7 initial ADRs:
  1. Native Go router instead of Envoy
  2. PostgreSQL as source of truth
  3. Immutable route snapshots
  4. Server-streaming gRPC for configuration distribution
  5. Eventual consistency for router configuration
  6. RBAC before full OPA integration
  7. REST for management API, gRPC for internal data-plane operations
- `docs/architecture.md` — skeleton with a Mermaid diagram of the target
  (not-yet-built) system.
- `docs/learning-guide.md` — skeleton, structure only.
- `README.md` — skeleton covering the sections required by the project
  brief (overview, problem statement, architecture, quick start, etc.).
- `LICENSE` (MIT).
- `.gitignore`.

## Testing

- `go build ./...`
- `go vet ./...`
- `golangci-lint run`
- `make run-control-plane` then `curl localhost:8080/healthz`
- One table-driven Go test for the health handler.

## Out of scope for Phase 0

Everything else in the project brief: tenant/agent models, database,
routing, gRPC agent contracts, config distribution, multi-tenancy/authz,
drift detection, reliability features, observability stack, mTLS,
dashboard, Kubernetes, performance work. These are Phases 1–12.
