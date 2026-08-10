# ADR 0002: PostgreSQL as Source of Truth

Status: Accepted

## Context

The control plane must durably store tenants, agents, routes, policies,
invocations, and configuration snapshots, support relational queries (e.g.
"list invocations for tenant X in the last hour"), and enforce per-tenant
row scoping.

## Decision

PostgreSQL is the single source of truth for all control-plane state.

## Alternatives Considered

- **etcd or Consul** — rejected: well suited to small KV configuration
  blobs, but weak for relational queries, joins, and reporting.
- **A NoSQL document store** — rejected: routes, destinations, and policies
  are inherently relational; a document store would push joins into
  application code for no benefit.
- **Multiple databases split by concern** — rejected as premature; a single
  PostgreSQL instance is sufficient at this project's scale (YAGNI).

## Consequences

Enables PostgreSQL Row-Level Security as a defense-in-depth layer for
tenant isolation, and transactions for atomic configuration-snapshot
creation. Redis (introduced in later phases) is used only for ephemeral
state — rate-limit counters, short-lived idempotency records — never as a
second source of truth.
