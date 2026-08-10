# ADR 0005: Eventual Consistency for Router Configuration

Status: Accepted

## Context

Multiple router instances exist. The control plane cannot guarantee that
every router applies a new configuration version at the same instant.

## Decision

AgentMesh accepts eventual consistency between desired configuration (held
by the control plane) and applied configuration (held by each router). The
control plane tracks per-router applied-version drift explicitly rather
than assuming updates are instantaneous.

## Alternatives Considered

- **Strong consistency via two-phase commit or quorum ack before serving
  traffic** — rejected: unnecessary coordination overhead for routing
  configuration that tolerates brief staleness, and would make the entire
  data plane unavailable if any single router is slow to acknowledge.
- **External config management (e.g. Kubernetes ConfigMaps + reload)** —
  rejected: reinvents the same eventual-consistency problem with less
  visibility into drift, and doesn't work for the Docker Compose-based
  local development environment.

## Consequences

A route or agent change is not instantly reflected on every router.
AgentMesh treats desired-vs-applied drift as a first-class, observable
state (surfaced via `config status` / `config drifts`, implemented in
Phase 6) rather than hiding it. The full consistency model is documented in
`docs/consistency-model.md` once drift detection ships.
