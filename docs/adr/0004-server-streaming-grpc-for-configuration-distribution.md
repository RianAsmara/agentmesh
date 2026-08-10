# ADR 0004: Server-Streaming gRPC for Configuration Distribution

Status: Accepted

## Context

Routers need to receive new configuration snapshots from the control plane
promptly, without the control plane tracking per-router polling schedules
and without every router hammering a REST endpoint on a tight loop.

## Decision

Routers open a long-lived server-streaming gRPC call
(`ConfigurationService.WatchConfiguration`) to the control plane; the
control plane pushes a new `ConfigurationSnapshot` message whenever the
router's tenant scope changes. Acknowledgement is a separate unary RPC
(`AcknowledgeConfiguration`) rather than part of a bidirectional stream.

## Alternatives Considered

- **Client polling over REST** — rejected: forces a trade-off between
  staleness and load that push-based streaming avoids.
- **A pub/sub broker (Kafka, NATS)** — rejected: adds an operational
  dependency for a v1 project where gRPC streaming already solves the
  problem.
- **Bidirectional streaming** — rejected: acknowledgements fit naturally as
  a separate unary RPC, keeping the watch stream simple and
  one-directional.

## Consequences

The router must handle stream reconnection (control-plane restarts,
network blips) and fall back to its last-applied snapshot meanwhile.
Delivery is at-least-once, not exactly-once — snapshot application must be
idempotent, which checksums make cheap to guarantee (re-applying the same
version is a no-op).
