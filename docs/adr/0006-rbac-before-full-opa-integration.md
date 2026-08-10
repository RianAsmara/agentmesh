# ADR 0006: RBAC Before Full OPA Integration

Status: Accepted

## Context

Authorization must gate both management API access (tenant/agent/route
CRUD) and data-plane invocation access (which identity may invoke which
capability). A general-purpose policy engine (OPA/Rego) is powerful but
adds real complexity: a new policy DSL, a policy-loading and versioning
story, and a new failure mode (policy evaluation errors).

## Decision

Implement role-based access control first (platform admin, tenant admin,
developer, viewer, agent service account, router service account) behind
an internal `authorization.Decider` interface. OPA, or another Rego-based
engine, is added later as an alternate or additional implementation of
that same interface.

## Alternatives Considered

- **Start with OPA/Rego from day one** — rejected for v1: the added
  operational and cognitive overhead isn't justified before basic
  tenant/role checks exist and are tested.
- **No authorization abstraction, ad hoc checks in handlers** — rejected:
  turns every denied-request code path into a special case and conflicts
  with the requirement that every denial be observable through consistent
  logging, metrics, and invocation records.

## Consequences

v1 authorization logic is simpler to reason about and test, at the cost of
being less expressive than Rego (no arbitrary attribute-based policies
yet). Because the `Decider` interface is fixed up front, adding OPA later
does not require changing every call site that checks authorization.
