# ADR 0003: Immutable Route Snapshots

Status: Accepted

## Context

The router evaluates a routing decision on every invocation and must do so
under concurrent reads while new configuration arrives from the control
plane. A mutex-guarded mutable route table would serialize the hot
invocation path against configuration updates.

## Decision

Represent applied router configuration as an immutable, versioned
snapshot. A configuration update builds an entirely new snapshot object and
swaps it in atomically (e.g. `atomic.Pointer`); readers never block on
writers and never observe a partially-updated configuration.

## Alternatives Considered

- **Mutex-guarded mutable maps** — rejected: readers and writers contend on
  the hot path, and holding a lock across a multi-field update risks torn
  reads.
- **Per-request database lookups** — rejected: makes routing latency depend
  on database availability and latency, defeating the purpose of a fast
  data-plane router.

## Consequences

Routing reads are lock-free with respect to concurrent configuration
updates, and the router can keep serving its last-known-good snapshot if
the control plane becomes unavailable. Cost: every configuration update
allocates a new snapshot — acceptable, since updates are rare relative to
invocation volume.
