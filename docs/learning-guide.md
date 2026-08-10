# AgentMesh Learning Guide

This is the primary teaching document for the project. For every major
feature, once implemented, this guide explains: what problem it solves, why
it exists, how it works, which files are involved, how to test it, common
failure modes, how it relates to real distributed systems, alternative
approaches, and likely interview questions.

Entries are added phase by phase, as each feature is actually built — this
guide never describes behavior ahead of the code that implements it.

## Phase 0 — Bootstrap

### Structured JSON Logging

**What problem it solves:** every AgentMesh service needs consistent,
machine-parseable logs (service name, level, message, structured fields)
instead of ad hoc `fmt.Println` output.

**Why it exists:** structured logs are what make the later observability
work (Phase 8) — correlating logs with traces and metrics by request ID —
possible at all.

**How it works:** `internal/observability.NewLogger(w io.Writer, service,
level string) *slog.Logger` wraps the standard library's `log/slog` with a
`slog.NewJSONHandler`, pre-attaches a `service` field via `.With(...)`, and
maps a string level (`"debug"`, `"info"`, `"warn"`, `"error"`) to an
`slog.Level`, defaulting unknown values to `info`.

**Files involved:** `internal/observability/logger.go`,
`internal/observability/logger_test.go`.

**How to test it:** `go test ./internal/observability/... -v`. The tests
write to an in-memory `bytes.Buffer` (not `os.Stdout`) so log level
filtering and field presence can be asserted directly.

**Common failure modes:** passing an invalid level string silently falls
back to `info` rather than erroring — this is intentional (logging setup
should never itself crash a service), but it means a typo'd
`AGENTMESH_LOG_LEVEL` fails silently rather than loudly. Startup validation
in `internal/config` (see below) is what actually catches that typo.

**How it relates to real distributed systems:** production services almost
universally emit structured (usually JSON) logs so a log aggregator
(Loki, Elasticsearch, CloudWatch Logs Insights) can index and query
individual fields instead of grepping free text.

**Alternative approaches:** Zap (higher throughput, more allocation-tuned,
but another dependency and a different API from the stdlib); Logrus (older,
slower, mostly superseded by `slog` since Go 1.21). `slog` was chosen to
follow the project's own preference for standard-library patterns where
practical.

**Interview questions this might raise:** Why JSON logs over plain text?
What's the cost of structured logging vs. `fmt.Println`? How would you add
request-scoped fields (e.g. `trace_id`) to every log line inside a single
request's handler chain without passing the logger through every function
signature? (Hint for later phases: `context.Context`-carried logger, or
`slog.Logger.With` per request.)

### Environment-Based Configuration with Startup Validation

**What problem it solves:** services need runtime configuration (listen
address, log level, timeouts) that varies between local development,
tests, and (eventually) containers/Kubernetes — without a config file
format to parse and without silently running with a nonsensical value.

**Why it exists:** "fails fast at startup with a clear error" is much
cheaper to debug than "started fine, but requests behave strangely because
`AGENTMESH_LOG_LEVEL=verboes` was silently ignored."

**How it works:** `internal/config.Load() (*Config, error)` reads
environment variables (`AGENTMESH_HTTP_ADDR`, `AGENTMESH_LOG_LEVEL`,
`AGENTMESH_SHUTDOWN_TIMEOUT`), falls back to defaults for anything unset,
and calls an internal `validate()` method that rejects unknown log levels,
an empty address, or a non-positive shutdown timeout before `Load` ever
returns successfully.

**Files involved:** `internal/config/config.go`,
`internal/config/config_test.go`.

**How to test it:** `go test ./internal/config/... -v`. Tests use
`t.Setenv` (automatically restores the previous environment after each
test, and marks the test as non-parallel-safe) to exercise defaults,
overrides, and each validation failure path.

**Common failure modes:** forgetting that `t.Setenv` cannot be combined
with `t.Parallel()` in the same test — if a future test in this package
needs to run in parallel, it must not also call `t.Setenv`.

**How it relates to real distributed systems:** this is the same
"twelve-factor app" configuration pattern (config via environment,
validated at process start) used by virtually every container-native
service, because it works identically in a local shell, a Docker Compose
service, and a Kubernetes pod spec's `env:` block.

**Alternative approaches:** a YAML/TOML config file (adds a parsing
dependency and a file-path-resolution problem — where does the file live
in a container?); a flags-based CLI config library like `spf13/viper`
(more features than needed here, and another dependency, contra the
project's stdlib-first preference).

**Interview questions this might raise:** Why validate at startup instead
of at first use? What's the trade-off between environment variables and a
config file for secrets specifically? How would you support hot-reloading
configuration without restarting the process, and what parts of `Config`
would be unsafe to hot-reload (e.g. `HTTPAddr`, which is bound to a
listening socket) versus safe (e.g. `LogLevel`)?

### Graceful Shutdown

**What problem it solves:** a service killed mid-request (e.g. by a
Kubernetes pod eviction or a plain Ctrl-C) should finish in-flight work
rather than dropping connections abruptly.

**Why it exists:** this is a baseline production-readiness requirement
called out explicitly in the project's reliability goals — it matters even
more once the router is forwarding live agent invocations.

**How it works:** `cmd/control-plane/main.go`'s `run` function uses
`signal.NotifyContext` to build a `context.Context` that's cancelled on
`SIGINT`/`SIGTERM`, runs `http.Server.ListenAndServe` in a goroutine
reporting to an error channel, and on either an unexpected server error or
a shutdown signal, calls `http.Server.Shutdown` with a bounded
`context.WithTimeout(cfg.ShutdownTimeout)` so shutdown can't hang forever.

**Files involved:** `cmd/control-plane/main.go`.

**How to test it:** manually — `go run ./cmd/control-plane`, then send
`SIGINT` (Ctrl-C) or `SIGTERM` and confirm the "shutdown signal received"
and "control-plane stopped cleanly" log lines appear and the process exits
0. (No automated test for this yet; Task 4's automated tests cover routing
only. Testing signal-driven shutdown deterministically is worth revisiting
once the router, which has real in-flight-request stakes, is built in
Phase 3.)

**Common failure modes:** a `ShutdownTimeout` set too low truncates
legitimate in-flight requests; set to zero or negative, `config.Load`
rejects it at startup rather than allowing a server that shuts down
instantly.

**How it relates to real distributed systems:** this is exactly what
Kubernetes expects from a well-behaved pod: on `SIGTERM`, stop accepting
new work, finish what's in flight within the grace period, then exit —
otherwise the kubelet sends `SIGKILL` after `terminationGracePeriodSeconds`
and in-flight requests are simply dropped.

**Alternative approaches:** immediately closing all connections on signal
(simpler, but drops in-flight requests — unacceptable once real traffic
flows through this service); a supervisor/process-manager-driven restart
strategy instead of in-process signal handling (pushes the same problem
into infrastructure rather than solving it in the service).

**Interview questions this might raise:** What's the difference between
`http.Server.Close()` and `http.Server.Shutdown(ctx)`? What happens to a
request that's still running when the shutdown timeout expires? How does
this interact with a Kubernetes readiness probe (hint: Phase 7 adds
readiness/liveness endpoints — readiness should flip to "not ready"
*before* shutdown begins, so the load balancer stops sending new traffic
ahead of the process actually stopping).

### Protobuf Tooling (buf)

**What problem it solves:** protobuf/gRPC code generation via raw `protoc`
invocations is fragile (plugin versions, include paths, per-language flags
all have to match by hand across every developer's machine and CI).

**Why it exists:** Phase 2 defines real gRPC services on top of this
tooling; setting up the config now means Phase 2 is "write `.proto`, run
`make generate`," not "first invent a build pipeline."

**How it works:** `api/proto/buf.yaml` declares the module and enables
standard lint/breaking-change rules; `api/proto/buf.gen.yaml` declares two
local plugins (`protoc-gen-go`, `protoc-gen-go-grpc`) generating into
`api/proto/gen/go` with `paths=source_relative`. `make generate` installs
`buf` and both plugins via `go install` if missing, then runs `buf
generate`.

**Files involved:** `api/proto/buf.yaml`, `api/proto/buf.gen.yaml`,
`api/proto/agentmesh/v1/common.proto`, the `generate` target in `Makefile`.

**How to test it:** `make generate`, then confirm
`api/proto/gen/go/agentmesh/v1/common.pb.go` exists and `go build ./...`
still succeeds. This requires network access to the Go module proxy the
first time it runs (to install `buf` and the plugins); see the note in the
Phase 0 implementation plan if that step fails in a sandboxed environment.

**Common failure modes:** editing generated `.pb.go` files by hand — any
such edit is silently discarded the next time `make generate` runs. Proto
field renumbering or removal without following buf's breaking-change rules
(`buf breaking`) can silently break wire compatibility with already-deployed
clients.

**How it relates to real distributed systems:** schema-first API design
with generated, strongly-typed client/server code (protobuf, plus
alternatives like Thrift or Avro) is the standard way large systems keep
service boundaries in sync without hand-written serialization code, and
`buf`'s breaking-change detection is what stops an accidental
backward-incompatible field change from shipping.

**Alternative approaches:** raw `protoc` + shell scripts (more moving parts
to keep in sync across machines and CI, no built-in lint/breaking-change
checks); the Buf Schema Registry's remote plugins instead of locally
installed ones (removes the local `go install` step but adds a runtime
dependency on a third-party registry being reachable at generate time,
which is a worse fit for this project's "runs offline once dependencies are
cached" goal).

**Interview questions this might raise:** Why protobuf over JSON for
internal service-to-service calls? What does `paths=source_relative`
actually control? How does `buf breaking` know a change is
backward-incompatible, and what categories of proto change are always safe
(adding a new optional field) versus never safe (reusing a field number)?

## Phases 1–12

Not yet written — each phase's section is added here when that phase ships,
following the same structure as the Phase 0 entries above.
