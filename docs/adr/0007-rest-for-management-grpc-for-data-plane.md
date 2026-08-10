# ADR 0007: REST for Management API, gRPC for Data-Plane Operations

Status: Accepted

## Context

AgentMesh exposes two different kinds of API surface: human/CLI-facing CRUD
management (tenants, agents, routes) and machine-to-machine,
latency-sensitive data-plane operations (agent invocation, configuration
streaming, health and status reporting).

## Decision

Use a versioned REST/JSON API (`/v1/...`) for control-plane management
operations, and gRPC/protobuf for all internal data-plane and
control-plane-to-router communication (`AgentRouter`, `AgentService`,
`ConfigurationService`).

## Alternatives Considered

- **gRPC-only, fronted by grpc-gateway for REST** — rejected: adds a
  translation layer and generated-gateway complexity that isn't justified
  for what is fundamentally CRUD traffic from a CLI and dashboard.
- **REST-only, including router-to-agent invocation** — rejected: loses
  HTTP/2 multiplexing, native streaming, and strongly-typed contracts that
  matter for the high-frequency invocation path and for server-streaming
  configuration distribution.

## Consequences

Two API styles must be documented and maintained (OpenAPI for REST, `.proto`
for gRPC) instead of one, but each is used where it fits best: REST's
ubiquity and tooling for management, gRPC's performance and streaming for
the data plane. This is also a deliberate portfolio choice — it
demonstrates both API design disciplines rather than only one.
