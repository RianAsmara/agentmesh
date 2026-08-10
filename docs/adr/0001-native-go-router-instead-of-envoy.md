# ADR 0001: Native Go Router Instead of Envoy

Status: Accepted

## Context

AgentMesh needs a data-plane router that performs capability-based routing,
tenant isolation, and reliability policies (retries, timeouts, circuit
breakers) between agent clients and destination agents. Envoy, driven by
xDS, is the industry-standard proxy for this class of problem.

## Decision

Build a native Go data-plane router for v1 instead of adopting Envoy/xDS.

## Alternatives Considered

- **Envoy with an xDS control plane** — rejected for v1: implementing an
  xDS server is a significant undertaking on its own, and delegating
  routing to Envoy would hide the Go distributed-systems work this project
  exists to demonstrate.
- **Istio or another service-mesh sidecar** — rejected: even heavier than
  bare Envoy, and couples routing to Kubernetes, while AgentMesh must also
  run standalone via Docker Compose.

## Consequences

Full control over routing, authorization, and observability logic lives in
Go, which is the point of the project. In exchange, AgentMesh reimplements
things Envoy provides for free (extensive load-balancing algorithms, an
admin API, years of HTTP/2 edge-case handling). The configuration-snapshot
format and the gRPC configuration-distribution API (ADR 0004) are
deliberately modeled after xDS-style push/ack patterns so an Envoy/xDS data
plane could be introduced later without redesigning the control plane.
