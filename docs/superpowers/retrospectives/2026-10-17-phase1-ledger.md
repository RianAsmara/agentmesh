# SDD ledger — plan: docs/superpowers/plans/2026-10-14-agentmesh-phase1-registry.md

Spec: docs/superpowers/specs/2026-10-14-agentmesh-phase1-design.md (reachable — rulings are binding, not provisional)
Worktree: .claude/worktrees/phase1-registry, branch worktree-phase1-registry
Merge base: a0770a5

## Pre-flight scan

### Shared files across tasks

| File | Tasks | Produce vs consume | Finding |
|---|---|---|---|
| `internal/api/dto.go` | 7 creates, 8 modifies | T7 writes tenant DTOs + `decodeJSON`/`parseLimit`/`listResponse`; T8 appends agent DTOs | OK — append-only, no redefinition |
| `internal/store/postgres/testutil_test.go` | 3 creates, 4+5 consume | T3 writes `testDB`+`newTenant`; T4/T5 call both | FINDING 1, 2 |
| `internal/store/postgres/cursor.go` | 4 creates, 5 consumes | T4 writes `encodeCursor`/`decodeCursor`/`clampLimit`; T5 calls all three | OK |
| `internal/config/config.go` | Phase 0 owns, 9 modifies | T9 adds DatabaseURL + pool fields | OK — T9 Step 4 explicitly repairs the Phase 0 tests it breaks |
| `cmd/control-plane/main.go` | Phase 0 owns, 9 rewrites | T9 replaces `newMux` wiring with `api.NewRouter` | OK — T9 Step 8 replaces the now-obsolete `main_test.go` |
| `Makefile` | 2, 9 modify | T2 wires `migrate`; T9 wires `dev`/`seed`/`compose-down`/`run-control-plane`/`test-short` | OK — disjoint targets |

### Per-task internal consistency

| Task | Tests vs code specified | Files created vs later touched | Finding |
|---|---|---|---|
| 1 | 7 test funcs, all 5 source files defined | self-contained | clean |
| 2 | 3 test funcs; `mustExec` defined once | `migrate.Run`/`Applied` used by T3 testutil + T9 | clean |
| 3 | `translateErr` test only; testutil has no test of its own | testutil consumed by T4, T5 | FINDING 1, 2, 3 |
| 4 | cursor + 9 TenantStore tests | `newTenant` from T3 | clean |
| 5 | 14 AgentStore tests | consumes T3 + T4 helpers | clean |
| 6 | 7 middleware tests | `Chain`/`LoggerFrom` consumed by T7, T8 | clean |
| 7 | 3 error + 16 tenant handler tests | fake stores reused by T8 | clean |
| 8 | 15 agent + 5 router tests | reuses T7 `doRequest`/`decodeErrorBody`/`seedTenant` | clean |
| 9 | 3 config + 2 main tests | rewrites Phase 0 test file | clean |
| 10 | docs only, 3 grep invariant checks | no code | clean |

### Findings and rulings

**FINDING 1 — name mismatch.** Task 3's Produces block names the helper
`newTestStore(t *testing.T) *gorm.DB`; the code in Step 9 defines it as
`testDB`. Tasks 4 and 5 call `testDB`.

Ruling: `testDB` is the name — the code block and both consumers agree, and
only the summary line disagrees. Carry this correction in the Task 3
dispatch. Cost if wrong: a rename across three files, caught at compile time.

**FINDING 2 — wrong step reference.** Task 3 Step 9's note says the file
"will not compile until Task 4's Step 3 lands". `NewTenantStore` is written
in Task 4 **Step 7**; Step 3 is the cursor helpers.

Ruling: the correct reference is Task 4 Step 7. Cosmetic, but the note is
there to stop an implementer chasing a phantom compile error, so it must
point at the right step. Cost if wrong: minutes of confusion, nothing more.

**FINDING 3 — Task 3 leaves a non-compiling test package (structural).**
`newTenant` calls `NewTenantStore`, which does not exist until Task 4. As
written, Task 3 commits a `_test.go` file that cannot build, so Task 3's own
review sees a broken package and `go test ./...` fails for anyone who checks
out that commit. The plan acknowledges this (Step 10 builds non-test code
only) but acknowledging it does not make the tree green.

Ruling: split the helper by owner. Task 3's `testutil_test.go` contains only
`testDB` and the shared-container machinery — everything that compiles
against Task 3's own code. Task 4 Step 5 appends `newTenant` to that same
file, in the task where `NewTenantStore` is born. Every task then ends on a
tree where `go build ./... && go test ./...` passes, which is the invariant
the per-task review gate depends on. Both dispatches carry this instruction.

Cost if wrong: trivial and self-correcting — if Task 4 forgets to add
`newTenant`, Task 4's own tests fail to compile immediately.

(Unresolved-consume entries for Tasks 5-8 in the automated scan were
false positives: the Produces lines qualify names as `postgres.encodeCursor`
/ `api.LoggerFrom` while the consuming tasks list them bare.)

### Rulings applied to briefs before dispatch

- FINDING 1 → `task-3-brief.md` Produces line corrected to `testDB`.
- FINDING 2 → obsolete "Task 4's Step 3" note deleted outright (superseded
  by FINDING 3's fix, which removes the non-compiling condition entirely).
- FINDING 3 → `task-3-brief.md` Step 9 now writes `testutil_test.go` with
  `testDB` only, and its Step 10 verifies `go build && go vet && go test
  -short` all pass. `task-4-brief.md` Step 5 now appends `newTenant` to that
  same file, in the task that creates `NewTenantStore`.

## Task log

BASE for Task 1: a0770a5

**Ruling: golangci-lint version mismatch (applies to every task).**
The PATH binary at `/home/asmaraputra/go/bin/golangci-lint` is v1.64.8, but
`.golangci.yml` declares `version: "2"` — v1 refuses to run against this
config, so the plan's `golangci-lint run ./...` gate does not execute as
written on this machine. Task 1's implementer worked around it by installing
v2.14.0 to `/tmp/golangci-v2-install/golangci-lint`; I verified that binary
runs clean against the repo (`0 issues`).

Decision: every task from here uses `/tmp/golangci-v2-install/golangci-lint
run ./...` as the lint gate, and the dispatch carries that path. I am NOT
upgrading the user's PATH binary — that is a side effect outside this
worktree, affecting every other Go project on the machine, and is the user's
call to make.

Cost if wrong: the `/tmp` binary is volatile; if it disappears mid-run a task
reinstalls it with `GOBIN=/tmp/golangci-v2-install go install
github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`. The lint gate
itself stays real either way. Surfaced to the user at the end: the repo's
lint config and their installed binary disagree, and that is worth fixing
permanently outside this phase.

Task 1: implementer DONE_WITH_CONCERNS (golangci-lint v1/v2 mismatch, ruled above;
`go mod tidy` hygiene commit b2695d8 beyond the brief's steps — no behavior change,
accepted). Report at task-1-report.md. Review package review-a0770a5..b2695d8.diff.
Task 1: task reviewer dispatched (sonnet) over a0770a5..b2695d8.
Task 1: review clean — spec compliant, Task quality Approved, 0 Critical, 0 Important.
Task 1: minor (deferred): hand-rolled `itoa` in internal/domain/validate.go reinvents
  strconv.Itoa and returns "" for negative input (harmless today — all call sites pass
  non-negative constants/indices; no test of its own).
Task 1: minor (deferred): go.mod has two separate require blocks (cosmetic).
Task 1: minor (deferred): no boundary test at exactly maxEndpointLen (512) or
  Tenant.Name 200 chars — inherited from the brief's own test list, not introduced.
Task 1: Ruling: reviewer's ⚠️ on `uuid.New()` having no call site in this diff is not a
  gap — Task 1 defines structs and validation only; the constraint binds the store layer.
  Carried into Task 4/5 dispatches to confirm IDs are generated in Go, not by DB defaults.
  Cost if wrong: a later task uses a PostgreSQL default and the store review catches it.
Task 1: complete (commits a0770a5..b2695d8, review clean)

BASE for Task 2: b2695d8
Task 2: Ruling: the /tmp golangci-lint v2 binary had vanished (as the standing ruling
  predicted). Reinstalled v2.14.0 to /tmp/golangci-v2-install before dispatch and
  verified --version. Lint gate unchanged; still not touching the PATH binary.
  Cost if wrong: none — reinstall is idempotent and local to /tmp.
Task 2: implementer dispatched (sonnet) from BASE b2695d8. Docker 29.9.0 verified up
  so the testcontainer steps can really execute.
Task 2: implementer DONE (4 commits b2695d8..381c9aa). Reported 7/7 migrate tests green
  against a real postgres:17-alpine container, -short skips cleanly, lint 0 issues.
Task 2: Ruling: editor diagnostics flagged "missing go.sum entry" (x/sync/semaphore,
  x/text/precis) and "undefined: Run/Applied" against this tree. Verified directly at
  381c9aa: go build, go vet, go mod verify, go test -short all clean; go doc lists both
  Run and Applied; go mod tidy is a no-op with a clean tree afterwards. Diagnostics were
  a stale pre-resolution editor cache, NOT real defects — not entering the fix loop, and
  the reviewer is told not to re-verify build/module state.
  Cost if wrong: the task reviewer reads the same diff and would catch a genuine break.
Task 2: task reviewer dispatched (sonnet) over b2695d8..381c9aa. Flagged the report's
  "byte-for-byte verbatim" claim and the image-pull flake for independent judgement.
Task 2: review clean — spec compliant (reviewer independently diffed all 5 files
  byte-for-byte vs the brief), Task quality Approved, 0 Critical, 0 Important.
  Schema verified: no DB-side UUID defaults, all timestamps TIMESTAMPTZ, both FKs
  ON DELETE CASCADE, all 4 indexes present. Transactional idempotency correct.
Task 2: controller closed the reviewer's one gap — ran the lint gate directly at
  381c9aa: /tmp/golangci-v2-install/golangci-lint run ./... => "0 issues."
Task 2: minor (deferred): postgres:17-alpine image-pull race can make the container
  tests flaky on a cold machine/CI; harness does not pre-pull or serialize. Revisit if
  it resurfaces or if t.Parallel() is ever added to this package.
Task 2: minor (deferred): no multi-migration ordering test (only 0001_init.sql exists).
  sort.Strings over zero-padded names is correct by construction but untested >1 file —
  whoever adds 0002_*.sql must add the ordering test.
Task 2: complete (commits b2695d8..381c9aa, review clean)

BASE for Task 3: 381c9aa
Task 3: dispatching with both preflight corrections carried (FINDING 1: helper is
  `testDB`, not `newTestStore`; FINDING 3: testutil_test.go holds ONLY testDB + the
  container machinery — `newTenant` belongs to Task 4 where NewTenantStore is born,
  so Task 3 must end on a tree where go build && go test -short both pass).
Task 3: implementer DONE (1 commit 381c9aa..57a1e53). TestTranslateErr 8/8; build, vet,
  test -short all pass on the final commit — the FINDING 3 invariant held.
Task 3: Ruling: lint reports 7 `unused` findings (not the 17 the report estimated), all
  in models.go (capabilityFromDomain, capabilityModel.toDomain) and testutil_test.go
  (sharedOnce, sharedDB, sharedErr, testDB, startSharedPostgres). Verified each is named
  in a later brief's Consumes line: task-5-brief.md:7 consumes capabilityFromDomain and
  testDB; task-4-brief.md:7 consumes testDB, and its Step 5 appends newTenant. These are
  transient dead-symbol warnings from splitting a package across tasks, NOT defects —
  not entering the fix loop. Zero bodyclose/gosec/revive findings. Re-checking the lint
  gate after Task 5 is the real test: any `unused` still standing then IS a finding.
  Cost if wrong: a genuinely orphaned helper survives to the final review, which runs
  the whole-branch lint and would surface it.
Task 3: Ruling: editor diagnostics again stale ("undefined: translateErr" in
  errors_test.go) plus a style hint (errors.As -> AsType[*pgconn.PgError], a Go 1.27
  idiom not required by the brief and not flagged by the configured linters). Neither is
  a defect; go build/vet/test -short are clean at 57a1e53.
  Cost if wrong: the task reviewer reads the same diff and would catch a real break.
Task 3: task reviewer dispatched (sonnet) over 381c9aa..57a1e53.
Task 3: review clean — spec compliant, Task quality Approved, 0 Critical, 0 Important.
  Reviewer cross-checked every GORM column tag against 0001_init.sql (no drift), every
  mapper field against the domain structs, GORM confinement (store.go imports no GORM),
  and forward-compat of every signature against the Task 4/5/9 call sites. Orphan-symbol
  check: none — every symbol is named in a later brief's Consumes line. TestTranslateErr
  builds real *pgconn.PgError values with real SQLSTATE codes (23505/23503/42P01),
  including %w-wrapped cases, not string matching.
Task 3: minor (deferred): internal/store/store.go:591-592 doc comment says
  "defaultLimit dan maxLimit" (lowercase) but the identifiers are exported
  DefaultLimit/MaxLimit. Verbatim from the brief, cosmetic.
Task 3: minor (deferred): implementer's report claimed 17 unused lint findings; the real
  count is 7 (controller-verified). Report narrative didn't match the tool output it
  quoted — implementers should paste exact output, not summarize from memory.
Task 3: complete (commits 381c9aa..57a1e53, review clean)

BASE for Task 4: 57a1e53
Task 4: implementer dispatched (sonnet) from BASE 57a1e53. Dispatch carries: the FINDING 3
  instruction to APPEND newTenant to the existing testutil_test.go (not recreate it), the
  uuid.New()-in-Go constraint from Task 1's carried ruling, the expectation that the 7
  transient `unused` findings shrink (with the exact remaining list to be pasted, not
  summarized — Task 3's report miscounted), and a warning that cursor pagination must be
  tested across a real page boundary, not just one page.
Task 4: implementer DONE (2 commits 57a1e53..bcf8266). Controller verified at bcf8266:
  build/vet clean, tree clean, container-backed `go test ./internal/store/...` ok (2.7s).
Task 4: Ruling: ACCEPT the implementer's one deviation from the brief's literal text —
  `.Truncate(time.Microsecond)` added to time.Now().UTC() in tenant.go:38 (Create) and
  :109 (Update), where the brief (lines 555, 626) had bare time.Now().UTC(). Verified the
  reasoning holds: tenant_test.go:124 asserts got.CreatedAt.Equal(createdAt) comparing a
  DB round-trip against the in-memory value, and PostgreSQL TIMESTAMPTZ stores only
  microsecond precision, so an untruncated nanosecond value mismatches deterministically.
  The brief's own test cannot pass without this. Task 5 writes agent timestamps the same
  way and must apply the same truncation — carried into the Task 5 dispatch.
  Cost if wrong: timestamps lose sub-microsecond precision that PostgreSQL could never
  have stored anyway; no information is actually lost.
Task 4: Ruling: lint `unused` went 7 -> 9, not down. The implementer's report claimed a
  drop "from Task 3's baseline of 17" — that baseline was itself wrong (Task 3's real
  count was 7, already corrected in this ledger). Verified all 9 are Task 5 plumbing:
  agentModel/capabilityModel + their TableName(), agentFromDomain, agentModel.toDomain,
  capabilityFromDomain, capabilityModel.toDomain, and newTenant — every one named in
  task-5-brief.md:7 or called at task-5-brief.md:52+. The count rose because Task 4 ADDED
  newTenant (a Task 5 consumer) while consuming only some Task 3 symbols. Not defects.
  The real gate is the post-Task-5 lint run: any `unused` still standing then IS a finding.
  Cost if wrong: the final whole-branch review runs lint and would surface a true orphan.
Task 4: NOTE — two implementer reports in a row have misstated their own lint output.
  Future dispatches must require pasted tool output; controller re-verifies independently.
Task 4: task reviewer dispatched (sonnet) over 57a1e53..bcf8266.
Task 4: review clean — spec compliant, Task quality Approved, 0 Critical, 0 Important.
  Reviewer independently confirmed the truncation ruling: pgx truncates (not rounds) to
  microseconds for TIMESTAMPTZ, matching time.Truncate exactly; applied consistently
  (CreatedAt+UpdatedAt on Create, UpdatedAt only on Update, CreatedAt untouched). Crucially
  it checked the cursor-boundary risk I flagged: encodeCursor is always built from
  last.CreatedAt read back off Postgres (tenant.go:92,84), already microsecond precision,
  so the Go-side truncation cannot affect keyset pagination. No boundary bug.
  Keyset predicate (created_at, id) > (?, ?) has a real UUID tiebreaker. translateErr
  applied on all five DB paths. Compile-time guard var _ store.TenantStore at tenant.go:26.
  uuid.New() in Go verified at every Create call site.
Task 4: minor (deferred): cursor.go:36-40 encodeCursor swallows a json.Marshal error into
  "" which List would read as "no next page". Unreachable today (payload is time.Time +
  string); revisit if cursorPayload gains a non-marshalable field.
Task 4: minor (deferred): no test forces two tenants to share an exact created_at, so the
  equal-timestamp tiebreaker is never deliberately exercised (tests sleep 2ms between
  creates). Verbatim from the brief, not an implementer deviation. Coverage gap.
Task 4: complete (commits 57a1e53..bcf8266, review clean)

BASE for Task 5: bcf8266
Task 5: implementer dispatched (sonnet) from BASE bcf8266. Dispatch carries the Task 4
  truncation ruling forward as binding (use .Truncate(time.Microsecond) even where the
  brief's literal text says bare time.Now().UTC()), the uuid.New()-in-Go constraint for
  BOTH agent and capability IDs, Review Focus item 4 (duplicate-capability create must
  roll back in ONE transaction leaving no orphan agent row — test must assert the absent
  row, not just the error), and the standing expectation that `unused` reaches 0 since
  Task 5 is the last consumer in the store layer. Exact lint output required pasted.
Task 5: implementer DONE (1 commit bcf8266..2db10a0). Controller verified at 2db10a0:
  build/vet clean, tree clean, and the standing lint gate finally resolves —
  /tmp/golangci-v2-install/golangci-lint run ./... => "0 issues." This CLOSES the Task 3
  and Task 4 `unused` rulings: every transient symbol has a real consumer, no orphans in
  the store layer. The truncation ruling was honoured (agent.go:45, :162) and Create runs
  in a single tx (agent.go:49 Transaction, :51 agent row, :64 capability rows).
Task 5: task reviewer dispatched (sonnet) over bcf8266..2db10a0.
Task 5: review clean — spec compliant, Task quality Approved, 0 Critical, 0 Important.
  All three high-risk behaviours verified by the reviewer: (1) transactional rollback —
  every insert scoped to tx (agent.go:68,81), translateErr never returns nil for non-nil
  input so the closure always rolls back, and the test proves absence via a post-failure
  GetByName => ErrNotFound; (2) capability filter uses a correlated EXISTS subquery
  (agent.go:133-136) so dedup is structural, not a bolted-on DISTINCT, with a test on a
  two-matching-capability agent; (3) pagination mirrors tenant.go's proven keyset pattern.
  Preload (agent.go:101,148) batches — no N+1. Compile-time guard at agent.go:42.
Task 5: minor (deferred): no test forces two agents to share an exact created_at, so the
  (created_at, id) tiebreaker is never exercised. Same gap as Task 4's tenant_test.go —
  a consistent pattern across the store layer, worth one test when next touched.
Task 5: minor (deferred): agent.go:68 tx.Omit("Capabilities") is moot — agentFromDomain
  never populates agentModel.Capabilities, so association auto-save could not fire. The
  comment implies the Omit is load-bearing when it is not.
Task 5: minor (deferred): THIRD consecutive implementer to misstate its own counts
  (claimed 15 tests, actual 16; the brief's prose says 14). No scope creep — all 16 of
  the brief's named tests are present verbatim. Reporting-accuracy pattern, not a defect.
Task 5: complete (commits bcf8266..2db10a0, review clean) — STORE LAYER DONE, lint 0 issues

BASE for Task 6: 2db10a0
Task 6: implementer dispatched (sonnet) from BASE 2db10a0. API LAYER BEGINS — internal/api
  does not exist yet. Dispatch carries: the net/http confinement constraint, the no-leak
  rule for recovery (panic details logged, never in the response body), bodyclose applying
  to httptest bodies in tests, the lint baseline now being 0 issues (so anything flagged
  is real and self-contained to this task), and four middleware-specific test demands
  (no panic text in body, well-formed response after recovery, incoming request-ID header
  honoured vs generated, implicit-200 status capture).
Task 6: implementer DONE (1 commit 2db10a0..ffa5f8f). Controller verified at ffa5f8f:
  build/vet clean, tree clean, lint "0 issues.", and `go list -deps ./internal/api`
  confirms no gorm/pgx leaked into the API layer. Spot-checked the security-relevant
  claim myself: WithRecovery (middleware.go:177) logs the panic value via slog (:186-187)
  but writes only a generic envelope (:194-195), so no panic detail reaches the client.
Task 6: task reviewer dispatched (sonnet) over 2db10a0..ffa5f8f.
Task 6: review NEEDS FIXES — 0 Critical, 2 Important (both plan-mandated, i.e. the brief's
  own prescribed code is the source). Controller verified both against the real code:
  (1) Composition order. Chain's fold makes arg 1 outermost. WithRecovery's deferred
      handler reads RequestIDFrom(r.Context()) off its OWN r (middleware.go:190), but
      WithRequestID only injects the ID into the copy it passes downward — so with
      recovery outermost every panic log carries an empty request_id. Worse, a panic
      unwinds past WithLogging's frame, so a crashed request gets NO summary line at all.
      CONFIRMED LOAD-BEARING: task-8-brief.md:1103-1106 wires exactly that order
      (WithRecovery, WithRequestID, WithLogging). Left alone, Task 8 ships blind panics.
  (2) Double-write. WithRecovery (middleware.go:193-195) unconditionally sets a header,
      calls WriteHeader(500) and writes the envelope. If a handler wrote part of a body
      and then panicked, a real net/http server logs "superfluous WriteHeader" and the
      client gets a corrupted non-JSON response. httptest.ResponseRecorder hides this,
      which is why the brief's own tests pass. statusRecorder already tracks a `wrote`
      flag (middleware.go:122) — the machinery for the guard exists, unused by recovery.
Task 6: Ruling: FIX BOTH, do not park. The spec is the binding authority and it requires
  that a 500 never echo internals and that errors use the single envelope — a corrupted
  half-body response violates both, and silent panic observability defeats the stated
  purpose of the logging middleware. The brief's authorship does not grade its own work.
  Fix = make WithRecovery self-sufficient rather than order-dependent: guard the write
  behind a response-started flag, and have recovery read the request ID from the request
  it actually serves. Then Task 8 wires recovery INNERMOST so a panicking request still
  gets its summary line. Carried into the Task 8 dispatch as a correction to its line 1103.
  Cost if wrong: a middleware ordering that differs from the plan's literal text; behaviour
  is strictly better observed and the envelope contract is preserved either way.
Task 6: fix round 1/5 dispatched to the original implementer.
Task 6: fix round 1/5 — the dispatch recorded above never landed (session interrupted;
  tree was clean at ffa5f8f with no follow-up commit). Controller implemented the fix
  inline rather than re-dispatching: the diagnosis was already complete and verified
  against the real code in the ruling above, so a fresh implementer would only have
  re-derived it.
Task 6: fix commit eaf5df4. Both Important findings closed:
  (1) Double-write — WithRecovery now wraps w in the existing statusRecorder and returns
      early if rw.wrote is set, so a partial-body panic leaves the client's bytes alone
      instead of appending an envelope. Chose the write guard (not merely reordering)
      so correctness no longer DEPENDS on composition order — the reorder is now an
      observability improvement, not a correctness prerequisite.
  (2) Ordering — WithRecovery documented as innermost; a test wires
      Chain(h, WithRequestID(), WithLogging(logger), WithRecovery(logger)) and asserts
      BOTH log lines carry request_id=req-xyz and the summary line reports status 500.
      This is the corrected wiring Task 8 must use at task-8-brief.md:1103.
Task 6: TDD evidence — wrote both tests first. Test (1) failed for the right reason
  (body = `{"items":[{"error":{"code":"internal"...` — envelope appended to partial body);
  test (2) passed on first run, which is expected: it pins down behaviour the reorder
  must preserve rather than behaviour that was broken. Both green after the fix.
Task 6: gates at eaf5df4 — go build, go vet, go test -short ./... (all packages ok),
  go test -race ./internal/api/... ok, /tmp/golangci-v2-install/golangci-lint run ./...
  => "0 issues." Tree clean. Review package review-ffa5f8f..eaf5df4.diff.
Task 6: NOTE for the final whole-branch review — the partial-body-panic path is a real
  net/http behaviour that httptest.ResponseRecorder cannot reproduce. The new test
  asserts the recorder's bytes are untouched, which is the observable proxy; it does not
  prove absence of "superfluous WriteHeader" on a live server. An httptest.NewServer
  integration case would, and belongs in Task 8 where the router is actually served.
Task 6: fix-round reviewer dispatched (sonnet) over ffa5f8f..eaf5df4.
Task 6: fix review APPROVED — 0 Critical, 0 Important, 1 Minor (pre-existing, below).
  Reviewer verified rather than re-read: it temporarily reordered the Chain to put
  recovery outermost, reran, and reproduced BOTH predicted failures exactly ("log
  produced 1 lines, want 2" plus request_id:"" in the panic line), then reverted and
  confirmed a clean tree. The ordering test is therefore not tautological.
  Finding 1 traced closed across all three handler shapes: WriteHeader-only,
  Write-only (implicit 200), and Header().Set-only. statusRecorder sets `wrote` BEFORE
  delegating in both WriteHeader and Write, so no gap exists even if the delegated call
  itself panics mid-write. The Header().Set-only case correctly still sends the envelope
  — no bytes have left yet, so overwriting is safe.
  Double-wrap question resolved: WithLogging's recorder A wraps recovery's recorder B;
  B.WriteHeader(500) sets B.status then calls A.WriteHeader(500) which sets A.status, so
  the 500 WithLogging reports is structurally guaranteed, not an artifact of the test.
  Regression check was done by diff, not just by running: `git diff -- middleware_test.go`
  is +82/-0, so all 7 original tests are byte-for-byte untouched.
  Gates confirmed independently at eaf5df4: build, vet, 9 test funcs + 4 subtests PASS,
  -race clean, golangci-lint v2.14.0 => "0 issues."
Task 6: minor (deferred, PRE-EXISTING not introduced here): wrapping http.ResponseWriter
  in statusRecorder drops the optional http.Flusher / http.Hijacker / io.ReaderFrom
  interfaces — Go does not promote a concrete type's extra methods through an embedded
  interface. The reviewer confirmed this with a scratch test AND established via
  `git show ffa5f8f:internal/api/middleware.go` that WithLogging already wrapped w before
  this fix round, so eaf5df4 adds a second wrap on an already-unreachable property rather
  than breaking something that worked. Risk LOW for Phase 1: repo-wide grep for
  flush/hijack/sse/websocket/streaming and a spec scan found nothing needing them.
  CARRY FORWARD: any future phase adding SSE, websockets, or long-lived streaming must
  add Flush()/Hijack() forwarding methods to statusRecorder, or route those endpoints
  around WithLogging/WithRecovery. Recorded here because the constraint is invisible at
  the call site and will bite silently (a Flush becomes a no-op, not an error).
Task 6: complete (commits 2db10a0..eaf5df4, review clean after 1 fix round)
  API LAYER FOUNDATION DONE — lint 0 issues, no gorm/pgx in internal/api.

BASE for Task 7: eaf5df4
Task 7: pre-flight verified before dispatch — every Consumes symbol really exists at BASE:
  store.TenantStore (store.go:49) and store.TenantFilter (:27); domain.ErrNotFound /
  ErrAlreadyExists / ErrInvalid (errors.go:19,21,23) and domain.ValidationError (:30) with
  a real Unwrap making errors.Is(err, ErrInvalid) true (:40) — which the brief's
  "validation error" => 400/"invalid" test row depends on. LoggerFrom present from Task 6.
  Lint binary verified present at v2.14.0 (the standing /tmp volatility ruling).
Task 7: implementer dispatched (sonnet) from BASE eaf5df4. Dispatch carries: the Task 6
  fix as binding context (WithRecovery is innermost and write-guarded, so handlers must
  not assume a panic mid-write yields an envelope); the append-only discipline on dto.go
  since Task 8 appends agent DTOs to the same file; the requirement that writeError never
  leak a raw store/driver error string into the body for the unknown-error case; and the
  standing requirement to PASTE exact tool output (four consecutive implementers have now
  misstated their own counts).
Task 7: implementer DONE (5 commits eaf5df4..7acdee9). Controller verified at 7acdee9:
  build/vet clean, tree clean, lint "0 issues.", `go list -deps ./internal/api | grep
  -E 'gorm|pgx'` empty (layering intact), -race ok, -short ./... no regressions.
Task 7: Ruling: ACCEPT deviation 1 (fake store stamps CreatedAt/UpdatedAt). Verified the
  reasoning independently: internal/api/tenant.go contains NO reference to CreatedAt,
  UpdatedAt, or time at all — the handler delegates timestamping entirely to the store,
  and postgres/tenant.go:38-40 does set both through the *domain.Tenant pointer before
  insert (documented there as deliberate, so the returned value matches what was stored).
  The brief's fake omitted that, so the brief's own TestTenantHandler_Create could not
  pass against the brief's own fake. Fix is confined to the test double; no produced or
  consumed symbol changed. This is the same class as Task 4's truncation ruling: the
  brief's literal text contradicts its own test, and the test encodes the real contract.
  Cost if wrong: a fake diverges from the real store in a way a later integration test
  would expose — but it now matches MORE closely than the brief's version did.
Task 7: Ruling: ACCEPT deviation 2 (test const `secret` -> `dbErrorText`). Did NOT take
  this on trust — restored the brief's original name in place, reran lint, and reproduced
  exactly: "internal/api/errors_test.go:67:8: G101: Potential hardcoded credentials
  (gosec) ... 1 issues: * gosec: 1". Then restored the file and confirmed a clean tree.
  gosec pattern-matches the IDENTIFIER name against a string literal; the literal is a
  fake pq error used to prove 500s don't leak internals, not a credential. With //nolint
  forbidden and 0 issues required, renaming was the only compliant route. The test's
  assertions are unchanged — it still greps the body for "idx_agents_tenant_name" and
  "pq:". Cost if wrong: none; a test-local constant name carries no contract.
Task 7: Ruling: deviation 3 (dto.go written in one pass rather than two edits) is a
  non-event — on-disk content equals the brief's two snippets concatenated. Noted only
  because the append-only discipline for Task 8 depends on the FINAL file shape, which
  is correct.
Task 7: Review focus 2 verified by the controller directly: classify() checks
  errors.As(&ValidationError) BEFORE errors.Is(ErrInvalid) (errors.go:75-78) and carries
  a comment saying why. Had the order been reversed, ValidationError's field/issue detail
  would be silently replaced by the generic invalid message — the ordering is load-bearing
  and now documented at the point of risk.
Task 7: NOTE — FIFTH consecutive implementer to misstate its own counts: reported
  "27 top-level + 23 subtests", actual is 28 top-level + 22 subtests (the 50 total PASS
  lines were right). Controller recounted from `grep '^=== RUN   Test' | grep -vc '/'`.
  This is now a reliable pattern, not an anomaly: implementer-reported counts in this
  project must be treated as unverified until recounted. Not a defect in the code.
Task 7: Ruling: editor diagnostic "errors.As can be simplified using
  AsType[domain.ValidationError] (errorsastype)" at errors.go:54 is the same Go 1.27 style
  hint already ruled on in Task 3 — not required by the brief, not flagged by any
  configured linter, and lint is 0 issues. Not a defect; not entering the fix loop.
Task 7: task reviewer dispatched (sonnet) over eaf5df4..7acdee9.
Task 7: task reviewer DIED before reporting — API 503/429 rate limit (reset 3m57s), no
  verdict produced. Task 7 is NOT reviewed by that dispatch. Re-dispatching after the
  security triage below, so the re-run can also adjudicate the one real finding.

## Security review triage (automated plugin, 11 findings over 4 files)

Controller triage against the spec. The spec's "Out of Scope for Phase 1" section
(docs/.../phase1-design.md:444-449) states verbatim: "Authentication and authorization
(Phase 5). Every endpoint in Phase 1 is unauthenticated; the README and learning guide
must state this plainly rather than leave it implied." Spec:204-210 further records that
the {slug} path segment BECOMES the authorization boundary in Phase 5 via one middleware,
explicitly rather than per-handler filtering.

NOT APPLICABLE — out of scope by explicit spec ruling (8 findings):
  dto.go #1 (HIGH, mass assignment / status transition authz), tenant.go #1 (HIGH, authn
  on Delete), #2 (HIGH, privilege escalation via status), #3 (HIGH, unauthenticated
  Create), #4 (MEDIUM, cross-tenant enumeration in List), tenant_test.go #1 (HIGH,
  no auth gate in newTenantMux).
  Every one of these asks for authentication, a principal in context, role checks, or
  tenant-scoped visibility. All six are Phase 5 deliverables by name. Adding them now
  would mean inventing an auth model Phase 5 is designed to specify — the exact
  speculative work the phase boundary exists to prevent. Phase 1 endpoints being
  unauthenticated is a DOCUMENTED, DELIBERATE state, not an oversight.
  Guard that this stays honest rather than forgotten: task-10-brief.md:152 already
  requires the README's Security Model section to state "every endpoint is currently
  unauthenticated" plainly. VERIFIED that line exists in the brief. I also verified the
  string is NOT yet present in README.md or docs/learning-guide.md — so Task 10 Step 4
  is load-bearing for this triage and must not be dropped. Carried into Task 10 dispatch.
  The suggested `domain.ErrForbidden` sentinel does not exist in internal/domain — these
  findings assume a domain model Phase 1 does not have.

NOT APPLICABLE — misreads the code (1 finding):
  tenant_test.go #2 (MEDIUM) claims the fake's Update hides a mass-assignment control
  because it stores the whole struct. Verified the real store: postgres/tenant.go Update
  uses .Updates(map[string]any{"name","status","updated_at"}) — a genuine three-column
  allowlist, so ID/Slug/CreatedAt are untouchable there. The handler reads the row via
  GetBySlug and only assigns Name/Status from the request, and patchTenantRequest exposes
  ONLY Name and Status (dto.go:45-48) with DisallowUnknownFields set, so {"id":...} or
  {"slug":...} is a 400, never a silent write. The fake is therefore laxer than the real
  store but no exploit path reaches it. ACCEPTED AS A MINOR TEST-FIDELITY GAP, deferred:
  tightening the fake to copy only mutable fields would make the double mirror the real
  allowlist and is worth doing when this file is next touched (Task 8 touches it).

REAL AND IN SCOPE — 1 finding, fix required:
  dto.go #2 (MEDIUM, information disclosure): decodeJSON's default branch returns
  fmt.Errorf("%w: request body is not valid JSON: %s", domain.ErrInvalid, err.Error()) —
  it splices the raw encoding/json error text into an error that writeError renders into
  the 400 response body. This is in scope because it is NOT an auth question: it is the
  single-envelope + no-internals contract the spec already binds in Phase 1, and Task 6's
  whole fix round was about not leaking internals. encoding/json messages can carry Go
  TYPE and FIELD names (e.g. `json: unknown field "foo"`, or "cannot unmarshal string
  into Go struct field createTenantRequest.slug of type int"), handing a client the
  internal struct shape. Note this is exactly the leak class Task 7's OWN
  TestWriteError_DoesNotLeakInternalDetail exists to prevent — but that test only covers
  the 500 path, so the 400 path slipped through untested.
  Ruling: FIX. Return a fixed client message, log the decoder detail at debug via
  LoggerFrom. Deferring is wrong here: it is cheap, it is the contract Phase 1 already
  owns, and leaving it means the envelope promise is false for every malformed request.

NOT APPLICABLE — defense-in-depth for a threat model Phase 1 lacks (1 finding):
  dto.go #3 (MEDIUM, Content-Type enforcement / CSRF). CSRF requires ambient credentials
  (a cookie or session) for a browser to attach — Phase 1 has no authentication at all,
  so there is nothing for a cross-site request to ride. The finding's own suggested fix
  says to pair it with "SameSite on any session cookie once authentication is added",
  conceding the dependency. Content-Type enforcement belongs with the auth work in
  Phase 5 where it actually buys something. DEFERRED, carried to the Phase 5 note.
  (Had the spec required Content-Type validation in Phase 1 I would fix it; it does not.)

  errors.go #1 (MEDIUM, CWE-532 sensitive info in logs): err.Error() logged at 500.
  DEFERRED, not a Phase 1 defect. Logging full cause server-side is the deliberate design
  (errors.go comment says so: the body is generic BECAUSE the log holds the cause), and
  it is the only way to debug a 500. The finding's real content — that store errors bake
  user-supplied slugs into messages like `update tenant %q` — is a log-scrubbing concern
  that needs a scrubber + a decision about PII in logs. Tenant slugs are operator-chosen
  identifiers, not secrets, and logs are not client-visible. Revisit alongside Phase 5.

Task 7: CORRECTION to my own triage ruling above on dto.go #2. I wrote that the decoder
  detail reaches the 400 body. It does NOT. Probed it directly with a throwaway test
  before writing any fix: classify()'s ErrInvalid branch (errors.go) returns the fixed
  string "the request is invalid" and DISCARDS the wrapped message, so all three
  malformed shapes already render exactly
  {"error":{"code":"invalid","message":"the request is invalid"}}.
  err.Error() did contain `json: unknown field "nonexistent_field"` and "...Go struct
  field createTenantRequest.slug of type string", but that text never left the process.
  The finding's own "latent / defense-in-depth" label was accurate and my restatement of
  it as a live leak was not. Recording this because the ledger is the audit trail: a
  wrong controller ruling that goes uncorrected is worse than the original finding.
Task 7: hardening commit 41648f6 applied anyway, on narrower grounds than my first
  ruling claimed:
  (1) The no-internals property currently holds only because classify() happens to drop
      the message. That is non-local — a future classify() branch surfacing ve.Error()-style
      detail for ErrInvalid would turn a latent leak live with no change to decodeJSON.
      Keeping the detail out of the error chain makes the property local to where the
      untrusted text enters.
  (2) The detail was being DISCARDED, not merely withheld — it reached neither client nor
      log. It now goes to LoggerFrom(ctx).Debug, so an operator debugging a rejected body
      has the only clue that explains it. This is a net observability gain, not just a
      security change.
  Honest test note: the three new dto_test.go cases PASS against the pre-fix code. They
  pin down behaviour that was already correct but untested — Task 7's own
  TestWriteError_DoesNotLeakInternalDetail covers only the 500 path, leaving the 400 path
  unguarded. They are regression tests, not proof of a fixed bug, and the commit message
  says so.
Task 7: gates at 41648f6 — build, vet, lint "0 issues.", -race ok, -short ./... all
  packages ok, tree clean. Review package review-eaf5df4..41648f6.diff (supersedes the
  eaf5df4..7acdee9 package; the reviewer should read the full range).
Task 7: task reviewer (re-dispatch) APPROVED — spec compliant, 0 Critical, 0 Important,
  4 Minor. The reviewer verified rather than asserted: it traced the pointer DTO through
  a scratch Go program to get real decode behaviour, and it tested my hardening commit
  adversarially as instructed — reverting decodeJSON's default branch, re-running the
  three new tests, and confirming all 3 PASS against pre-fix code, then restoring to
  byte-identical HEAD. That independently confirms my ledger correction above: these are
  regression tests, not proof of a fixed live bug.
  REST surface checked field by field against spec Section 3: 201 on create, 204 with an
  asserted-empty body on delete, {items, next_cursor} with items never nil (make), the
  error envelope a field-for-field match, snake_case throughout. No drift.
  Write-once discipline (the Task 6 carry-forward): all clean. List builds the entire
  response slice BEFORE its single writeJSON call, Delete writes once, and MaxBytesReader
  does not write to w on overflow for a vanilla ResponseWriter (its requestTooLarger
  fast-path is HTTP/2-only), so no hidden write precedes writeError.
  LoggerFrom-without-middleware verified safe: slog.Default() is non-nil by stdlib
  guarantee, and dto_test.go calls decodeJSON against a bare context, demonstrating the
  fallback live.
Task 7: minor (deferred): empty-body PATCH {} still calls store.Update unconditionally.
  CONTROLLER-VERIFIED in tenant.go — there is no "did anything change" guard, so the real
  postgres store (tenant.go:109) bumps updated_at on a semantically empty patch. The fake
  cannot catch this because it does not stamp UpdatedAt on Update, so
  TestTenantHandler_PatchEmptyBodyChangesNothing is blind to it and its name overclaims.
  Not a spec violation (spec is silent). Fix when next touched: guard the Update call on
  req.Name != nil || req.Status != nil, and teach the fake to stamp UpdatedAt so the test
  can see the difference. Logged as the clearest instance of the fake/real divergence
  pattern this phase keeps producing.
Task 7: minor (deferred): {"name":null} is indistinguishable from an absent field (Go
  sets *string to nil for explicit null). Defensible — Name has no legitimate cleared
  state — but untested, and it is precisely the edge dto.go's own pointer-DTO comment
  advertises. One test case when next touched.
Task 7: minor (deferred): TestTenantHandler_PatchRejectsSlugChange's name overclaims.
  The protection is emergent from decodeJSON's DisallowUnknownFields, not a dedicated
  guard in Patch. It would NOT catch a regression where a later task adds Slug *string to
  patchTenantRequest without separately guarding the handler. Relevant to Task 8, which
  modifies dto.go.
Task 7: minor (deferred): writeJSON's comment ("an error here means the connection
  already broke") is true only because every current call site passes plain structs of
  string/time.Time that json.Encode cannot fail on — not a general invariant. Same class
  as Task 6's Flusher carry-forward: a note for whoever adds a response type with a
  custom MarshalJSON or a map with non-string keys.
Task 7: minor (deferred): the new Debug line in decodeJSON is suppressed at the default
  Info level (observability parseLevel defaults to Info), so it is not a log-flooding
  vector as shipped — but it IS a client-triggered, unrate-limited log-line generator on
  an unauthenticated 4xx path IF debug is ever enabled in production. Carried to the
  Phase 5 note alongside the Content-Type/CSRF deferral.
Task 7: complete (commits eaf5df4..41648f6, review clean after 1 controller hardening
  commit). TENANT REST SURFACE DONE.

BASE for Task 8: 41648f6
Task 8: pre-flight — brief is 1155 lines; creates agent.go, router.go, agent_test.go,
  router_test.go and APPENDS agent DTOs to dto.go (the append-only file from Task 7).
Task 8: BLOCKING CORRECTION to the brief, carried into the dispatch. task-8-brief.md:1102-1107
  wires Chain(mux, WithRecovery, WithRequestID, WithLogging) — recovery OUTERMOST — and
  its comment argues for it ("Recovery paling luar supaya panic di middleware mana pun
  ikut tertangkap"). Task 6's review PROVED that ordering defective and the fix-round
  reviewer reproduced both failures empirically: with recovery outermost every panic log
  carries request_id:"" and a panicking request gets NO summary line at all (the panic
  unwinds past WithLogging's frame). The correct order is Chain(mux, WithRequestID(),
  WithLogging(cfg.Logger), WithRecovery(cfg.Logger)) — recovery INNERMOST — which is what
  internal/api/middleware.go's WithRecovery doc comment now prescribes and what
  TestWithRecovery_InnermostLogsRequestIDAndSummary locks in.
  The brief's stated concern (a panic in middleware itself escaping) is real but minor by
  comparison: WithRequestID and WithLogging are a header read, a uuid, and a log line —
  no realistic panic source — whereas handler panics are the case that matters, and those
  are fully covered innermost. Accepting a theoretical gap to keep every real panic
  observable is the right trade.
  Cost if wrong: a panic originating inside WithRequestID/WithLogging crashes the process
  instead of returning 500. If that is ever judged unacceptable, the answer is recovery at
  BOTH ends, not recovery outermost alone.
Task 8: implementer DONE_WITH_CONCERNS (4 commits 41648f6..0d1d42b). Controller verified
  at 0d1d42b: build/vet clean, tree clean, lint "0 issues.", no gorm/pgx in internal/api,
  -race ok, -short ./... no regressions, and the append-only gate on dto.go PASSES
  (`git diff 41648f6..HEAD -- internal/api/dto.go | grep '^-' | grep -v '^---'` is empty).
Task 8: counts VERIFIED CORRECT for the first time this phase — 51 top-level, 53 subtests,
  104 PASS, 0 FAIL, matching the report exactly. This breaks a five-task miscount streak.
  The difference: this dispatch gave the exact grep recipe to count with rather than
  asking for accuracy. Worth carrying into Task 9 and 10 dispatches verbatim.
Task 8: Ruling: OVERRIDE 1 applied correctly. router.go:89-92 wires Chain(mux,
  WithRequestID(), WithLogging(cfg.Logger), WithRecovery(cfg.Logger)) — recovery innermost.
  Verified the comment (router.go:67-88) was rewritten to the REAL rationale: it explains
  Chain's fold, the empty request_id, and the lost summary line, and cites
  TestWithRecovery_InnermostLogsRequestIDAndSummary. The brief's incorrect "paling luar"
  reasoning is gone, not merely contradicted by the code. Implementer confirmed the brief's
  router_test.go had no test asserting outermost order, so nothing needed adapting —
  consistent with there being no router-level panic test, which is itself a gap (below).
Task 8: Ruling: ACCEPT deviation 3 (fake List gains a cursor shape check). Verified against
  the real contract: postgres/cursor.go decodeCursor's FIRST gate is
  base64.RawURLEncoding.DecodeString returning ErrInvalid, and the fake now does exactly
  that with the same codec and the same sentinel. The brief's own fake ignored
  filter.Cursor entirely, so the brief's own "malformed cursor" subtest could not pass.
  This is now the FOURTH time a brief's test contradicts the brief's own fixture (Task 7
  Create timestamps, Task 7 Update allowlist laxity, Task 7 PatchEmptyBody blindness, and
  now this). The inline comment is honest that only the base64 shape is checked, not the
  JSON payload — it does not overclaim fidelity it lacks.
  Cost if wrong: the fake accepts a well-formed-base64 cursor whose JSON is garbage, where
  the real store 400s. That path is covered by TestAgentStore_ListRejectsBadCursor at the
  store layer, so no coverage is actually lost.
Task 8: Ruling: ACCEPT deviation 4 (nil-Pinger guard + test). I asked for it in review
  focus 3, so it is requested work, not scope creep. Verified handleReadyz returns 503
  with the generic "database is not reachable" body and logs the reason server-side for
  BOTH the nil case and the failing-Ping case — a nil interface would otherwise be a real
  runtime panic on .Ping. The reason detail is deliberately kept out of the body because
  readyz is unauthenticated; that matches the Task 6/7 no-internals contract.
Task 8: NOTE — implementer's honest caveat on tenant-vs-agent 404s: both resolve through
  the same classify() codeNotFound path with the identical message "the requested resource
  does not exist", so a client cannot tell "no such tenant" from "no such agent". It
  correctly declined to invent per-resource messages as out of scope. Flagging for the
  reviewer to judge against the spec rather than ruling on it myself.
Task 8: task reviewer dispatched (sonnet) over 41648f6..0d1d42b.
Task 8: NOTE — the automated background security review did NOT run on 41648f6..0d1d42b
  (API refused with HTTP 404; log at ~/.claude/security/log.txt). So this range, which is
  the one that makes the whole API surface reachable (router.go + agent handlers + the
  unauthenticated healthz/readyz endpoints), has had NO automated security pass.
  Consequence: the final whole-branch review must cover this range deliberately rather
  than assuming a scan happened. Carried into the Task 10 / final-review dispatch.
  Context for weighting it: the Task 7 scan produced 11 findings of which 8 were Phase 5
  auth work excluded by spec:448, 1 misread the real store's column allowlist, and 1 was
  latent rather than live — so the expected yield here is low, but "low" is not "verified".
Task 8: CORRECTION to my note above — the security scan DID run on this range after all
  (the HTTP 404 notice was one scan failing; later scans landed). It produced 4 findings
  over router.go and agent.go. The "no automated security pass" note above is withdrawn.
Task 8 security triage:
  router.go #1 (HIGH, missing authn on the route table) and agent.go #1 (HIGH, no tenant
  authorization in resolveTenant): NOT APPLICABLE, Phase 5 by spec:448 ("Every endpoint in
  Phase 1 is unauthenticated"). Same ruling as the eight Task 7 auth findings. Worth noting
  the code ALREADY marks the seam the finding asks for: agent.go's comment above
  resolveTenant says Phase 5's authorization check goes exactly there, and spec:204-210
  describes that single-choke-point design. The finding and the design agree; only the
  phase differs. The suggested `cfg.Verifier`/`WithAuthn` do not exist yet by design.
  agent.go #3 (MEDIUM, no cap on capability list): REAL BUT MINOR, deferred. Verified
  domain.Agent.Validate has no length cap on a.Capabilities. The blast radius is bounded by
  decodeJSON's 1 MiB MaxBytesReader — roughly 35k unique capabilities max — but that is
  still ~35k per-row INSERTs inside one transaction (postgres/agent.go:64 loops rather than
  batching). Not exploitable into unbounded growth; it is a slow-request/lock-duration
  concern on an endpoint that is unauthenticated only because Phase 1 says so. Fix when the
  store layer is next touched: a maxCapabilities const checked in Validate (so it fails
  before the tx opens) plus CreateInBatches. Logged, not fixed now — it is Task 1 + Task 5
  code, both already reviewed and approved, and changing domain validation mid-phase would
  invalidate those gates for a non-exploitable issue.
  agent.go #2 (HIGH, SSRF on Endpoint): the SSRF FRAMING is not applicable — nothing in
  Phase 1 dials Endpoint. It is stored text; the router that dials it arrives in Phase 3
  (domain/agent.go:47 says so). An allowlist rejecting loopback/link-local is a Phase 3
  decision that needs to know what deployment topology is legitimate, and guessing now
  would likely be wrong. HOWEVER, triaging it surfaced a GENUINE SPEC GAP, below.
Task 8: FINDING (controller, pre-existing, NOT introduced by Task 8) — spec:115 requires
  `Agent.Endpoint` to be "non-empty, **parses as a gRPC target**". domain/agent.go:68-73
  checks only non-blank and <=512 chars. The "parses as a gRPC target" half of the rule is
  NOT IMPLEMENTED, so "dns:///svc:9090" and "!!! not a target !!!" are equally accepted.
  This is Task 1 code that passed its own review (and the reviewer's ⚠️ on uuid.New there
  shows it was reading the Produces list, not cross-checking spec:115's table row by row).
  Not fixing it inside Task 8 — out of this task's scope, and the fix belongs with whoever
  owns the domain validation gate. Raised to the Task 8 reviewer to rule on scope, and
  carried to the final whole-branch review which must check spec:108-120's table field by
  field. Cost if left: Phase 3 discovers unparseable endpoints at dial time, in production,
  on rows the API promised were valid.
Task 8: task reviewer DIED on API 503/429 rate limit before reporting (second such death
  this phase; the Task 7 reviewer died the same way and succeeded on re-dispatch).
  Task 8 remains UNREVIEWED. Re-dispatching now. Task 9 stays gated behind it.
Task 8: task reviewer (re-dispatch) APPROVED — spec compliant, 0 Critical, 2 Important
  (both coverage gaps, not live defects), 3 Minor. All 12 spec routes present exactly once
  with correct methods and status codes; dto.go append verified by reading the diff, not
  by trusting the ledger.
Task 8: reviewer RULING on spec:115 accepted — my reading is correct, the gRPC-target
  parse is genuinely unimplemented, and it is Task 1 / domain-validation scope, NOT Task 8
  (Task 8 only calls agent.Validate() and stores what it returns). Severity Important for
  the system, zero live impact until Phase 3 dials. Does not count against Task 8.
  CARRIED to the final whole-branch review, which must walk spec:108-120's validation
  table row by row — the per-task gates structurally cannot catch it because each task
  reviewer reads its own brief's Produces list, not the spec's table.
Task 8: reviewer RULING on the tenant-vs-agent 404 ambiguity — NOT a finding. Spec does
  not require distinguishable 404s (Section 3's only worked error example is the 409), it
  matches the Task 7 tenant precedent, and collapsing them is mildly information-
  minimizing. Ruled, not just restated, as asked.
Task 8: minor (deferred): Go 1.22+ ServeMux matches against the ESCAPED path, so {slug}
  and {name} CAN capture a value containing a literal "/" via %2F — the reviewer proved it
  (slug="acme/evil" matched and returned 200). Not exploitable: slug/name charsets forbid
  "/" at creation, and resolveTenant/GetByName do literal lookups rather than further
  routing, so such a value cleanly 404s. Worth one comment near the route registrations
  for the next person who assumes a wildcard cannot contain a slash.
Task 8: minor (deferred): repeated ?capability=a&capability=b silently keeps only the
  first value (url.Values.Get semantics). Accidental, not spec-defined; the spec only ever
  shows a single value. Either document it or reject repeats if multi-value is ever wanted.
Task 8: BOTH Important findings FIXED by the controller in 3166d7e (tests only, no
  production code — the shipped behaviour was already correct; what was missing was any
  test that would notice it changing).
  (1) Router wiring order. CONFIRMED the gap myself first: restored the defective
      recovery-outermost order in router.go and the entire suite stayed green. New
      TestRouter_PanicIsRecoveredWithRequestIDAndSummary drives a panic through the real
      NewRouter handler (via a panickingTenantStore embedding the fake and overriding
      List) and asserts the two properties unique to recovery-innermost. VERIFIED it fails
      against the defective order: "log produced 1 lines, want 2". This is the gap that
      mattered most — Task 6 paid a full fix round for that ordering and nothing guarded it.
  (2) Agent pagination. fakeAgentStore.List now truncates at filter.Limit and returns a
      base64url cursor, mirroring the real store; sorted by Name first because map
      iteration order would otherwise make the test flaky (a real risk the reviewer's own
      repro did not have to face). Two tests added: limit honoured + returned cursor
      replays without a 400, and last page OMITS next_cursor from the JSON (checked via a
      map, so it tests omitempty rather than just an empty string). VERIFIED both fail when
      the handler's Limit or next is broken: "items = 3, want 2" and "next_cursor is empty".
  Gates at 3166d7e: build, vet, lint "0 issues.", 54 top-level / 53 subtests / 107 PASS /
  0 FAIL, -race ok, -short ./... clean, tree clean. Production code untouched (+162 test
  lines only, confirmed by git diff --stat).
Task 8: complete (commits 41648f6..3166d7e, review clean + both Important findings closed)
  CONTROL PLANE API SURFACE DONE AND REACHABLE.

BASE for Task 9: 3166d7e
Task 9: dispatching. Carries: the exact test-counting grep recipe (it worked — Task 8 was
  the first accurate count this phase); the config/main rewrite scope (T9 adds DatabaseURL
  + pool fields and replaces newMux wiring with api.NewRouter, and its Step 4/Step 8
  explicitly repair the Phase 0 tests it breaks); the warning that Task 9 must wire
  recovery INNERMOST if it touches any Chain call (same correction as Task 8's override);
  and the standing instruction to paste exact tool output.
Task 9: implementer DONE_WITH_CONCERNS (7 commits 3166d7e..8908211). Controller verified
  at 8908211: build/vet clean, tree clean, lint "0 issues.", no gorm/pgx in internal/api,
  -race ok, -short ./... every package ok (cmd/seed now present, no test files — expected).
Task 9: counts VERIFIED CORRECT again — 11 top-level (9 config + 2 control-plane), 7
  subtests, 18 PASS, 0 FAIL, matching the report. Second accurate count in a row; the grep
  recipe has held. Carry it into Task 10.
Task 9: Ruling: the DSN-redaction claim is TRUE and I verified it the hard way rather than
  by reading. Static check first: cfg.DatabaseURL appears exactly once outside tests
  (main.go:45, the connect call) and is never passed to a logger; the startup log line
  (main.go:59-62) carries only max_open_conns/max_idle_conns. Then probed the error path,
  which was the real risk since main.go:51 wraps it with %w: built a throwaway test
  dialling a closed port with password "SuperSecretPw123". Result:
    "open gorm: failed to connect to `user=user database=db`: 127.0.0.1:1 ... connection refused"
  pgx redacts the password itself before the string ever reaches our wrapping, so neither
  the password nor the full DSN can reach a startup error. Probe removed, tree clean.
  Cost if wrong: a password in a crash log or an operator's terminal — worth the 2 minutes.
Task 9: Ruling: ACCEPT deviation 2 (implementer wrote pinger_test.go itself; the brief
  specified only pinger.go's production code with no fixture). Writing a test for new
  production code is required by the TDD gate, not scope creep. Two findings inside it are
  worth recording because they are the kind of thing that usually goes unsaid:
  (a) its first failure-path test would have closed the SHARED testcontainer pool that the
      rest of the package reuses — it caught this before running broadly and switched to a
      cancelled context. That is exactly the Task 3 shared-container machinery being
      load-bearing in a way no brief mentions.
  (b) gorm.Open against an unreachable Postgres fails at Open time (the driver queries
      server version during Initialize), so a *gorm.DB wrapping a dead connection cannot be
      constructed without a live container — which bounds what the Pinger failure path can
      be tested with. Honest limitation, correctly documented rather than faked.
Task 9: Ruling: ACCEPT deviation 3 (7 commits vs the brief's 1). My dispatch's commit
  discipline explicitly overrode the brief's Step 15. Working as intended.
Task 9: Ruling: the middleware-order override needed no code change — main.go delegates
  the whole handler to api.NewRouter rather than assembling its own Chain, which is the
  outcome the override asked for. Verified router.go untouched in this range.
Task 9: NOTE — implementer reports a manual end-to-end run against real Docker Compose
  Postgres (make dev/migrate/seed, /healthz 200, /readyz 200, tenant list, capability
  filter, idempotent re-seed, /readyz 503 with Postgres stopped then 200 after restart,
  and a grep of JSON logs for the DSN password finding zero matches). I have NOT
  independently reproduced this — it needs the Docker stack up and is the only claim in
  this task resting on implementer testimony alone. Flagged to the reviewer to re-run,
  since Task 10 Step 5 depends on the README's Quick Start actually working.
Task 9: task reviewer dispatched (sonnet) over 3166d7e..8908211.
Task 9: task reviewer DIED on API 503/429 (THIRD reviewer death this phase). It got far
  enough to report one partial result: "Confirmed: 503 on readyz, 200 on healthz while
  Postgres is down." That is the Pinger behaviour I most wanted verified, but it is a
  fragment, not a verdict — the restart-recovery half and items 2-7 never ran. Task 9
  remains UNREVIEWED.
Task 9: INCIDENT — the dead reviewer left `agentmesh-postgres` running for ~8 hours, bound
  to 0.0.0.0:5432 with POSTGRES_PASSWORD=agentmesh, on a host whose LAN address is
  192.168.50.113. So a guessable-credential Postgres was reachable from the local network
  until I tore it down (`docker compose down`, confirmed no agentmesh containers remain).
  Root cause is twofold: (a) a subagent that dies mid-run cannot execute its own cleanup
  instruction, and (b) docker-compose.yml publishes 5432 on all interfaces, so the blast
  radius of ANY forgotten teardown is the whole LAN rather than the host. (a) is a harness
  reality I can only mitigate by checking for leftovers after every failed Docker-using
  dispatch — now doing that. (b) is a real defect and is fixed below.
  Surfaced to the user explicitly rather than quietly cleaned up: a credentialed database
  was briefly network-reachable, which they may want to weigh.

## Security triage round 3 (11 findings over 6 files, Task 9 range)

NOT APPLICABLE — Phase 5 by explicit spec ruling (spec:448), same as the 8+2 before:
  main.go #1 (HIGH, no authn on the /v1 surface). Its alternative (b) — gate /v1 behind
  AGENTMESH_ALLOW_UNAUTHENTICATED and bind to loopback — is a real design option, but it
  invents configuration the spec does not define for a state the spec documents as
  intended. Phase 1's whole point is a working registry; a flag defaulting to refusing to
  start would make the documented Quick Start fail.

REAL AND FIXED NOW (1):
  docker-compose.yml #1 (HIGH): `ports: "5432:5432"` publishes on 0.0.0.0. Not theoretical
  — it is exactly what exposed the leftover container above. Fixed to 127.0.0.1:5432.
  The weak password is left as-is deliberately: it is a local dev stack, the compose file
  is the only consumer, and requiring a generated secret would break `make dev` as a
  one-command local bring-up, which Task 10's README Quick Start depends on. Binding to
  loopback removes the exposure that made the password matter. Documented in the commit.

REAL, DEFERRED WITH A CARRIED NOTE (3):
  pinger.go #2 (MEDIUM, unbounded ping context): /readyz hands Ping the request context,
  so a hung DB ties up a pool waiter per probe with no deadline. Worth bounding (2s) and
  worth adding ReadTimeout/WriteTimeout to the http.Server. NOT doing it inside this
  triage: it is production code in the range currently awaiting review, and editing it now
  would mean the reviewer reviews my edit instead of the implementer's work. Carried as the
  first item for the post-review fix round.
  config.go #2 (MEDIUM, no upper bound on MaxOpenConns/ConnMaxLifetime): same reasoning —
  real, cheap, but belongs in the fix round, not ahead of the gate.
  seed/main.go #3 (MEDIUM, no guard against seeding a non-local DB): legitimate footgun.
  `make seed` against a production DSN would write a tenant named acme. A loopback-host
  check plus an explicit opt-in is the right shape. Carried to the fix round.

NOT APPLICABLE — misreads this codebase (3):
  main.go #3 and pinger.go #1 (MEDIUM, DSN/driver detail in errors): I already PROVED the
  password cannot reach a startup error — pgx redacts it itself, producing
  "failed to connect to `user=user database=db`". The ledger records the probe. The readyz
  path additionally never puts the cause in the response body (router.go logs it and
  returns a generic reason), which Task 8's review verified. No leak exists to fix.
  main.go #2 (MEDIUM, suspended tenants still usable): checked the spec. Line 74 defines
  TenantStatus as "active | suspended" and line 131 defaults the column to 'active', but
  the spec defines NO enforcement semantics for suspended anywhere — not in Section 3's
  endpoint table, not in the validation table. Enforcing a 404-on-suspended in
  resolveTenant would be me inventing product behaviour the spec never specified, in a
  phase whose authorization story is explicitly deferred. Flagged as a SPEC GAP for the
  phase close-out instead: the field exists with no defined meaning, which is worth
  resolving before Phase 5 builds authorization on top of it.

NOT APPLICABLE — contradicts the spec's own dev stack (2):
  config.go #1 and Makefile #2 (MEDIUM, require sslmode=require / forbid sslmode=disable):
  the local compose Postgres serves no TLS, so rejecting sslmode=disable would make the
  spec-mandated Quick Start impossible. Correct resolution is a deployment concern (enforce
  TLS where a real DB lives), not a config-validation rule that breaks local dev. Carried
  to the Phase 5 / deployment note.
  Makefile #2's narrower point — that three targets each duplicate the DSN literal — is a
  fair maintainability nit; folding them into one DEV_DSN variable is a Task 10 cleanup at
  most.
Task 9: controller fix 833bf80 — docker-compose.yml port bound to 127.0.0.1:5432. Verified
  empirically, not by reading: started the stack and read the published port back
  (127.0.0.1:5432->5432/tcp, previously 0.0.0.0:5432->5432/tcp), then tore it down.
  `docker compose config --quiet` clean. This is the only security finding from round 3 I
  fixed inline, because it is the one with demonstrated real-world impact.
Task 9: review package refreshed to review-3166d7e..833bf80.diff (supersedes the ..8908211
  one; reviewer reads the full range including my compose fix). Re-dispatching review #2.
Task 9: task reviewer (re-dispatch) APPROVED — spec compliant, 0 Critical, 0 Important,
  3 Minor. All 7 priority items reached, nothing truncated, and it tore the stack down
  immediately after item 1 as instructed. Controller verified cleanup independently: no
  agentmesh containers, no stray control-plane process, port 5432 free, tree clean.
Task 9: THE E2E CLAIM IS NOW INDEPENDENTLY REPRODUCED — this was the one thing resting on
  implementer testimony. Reviewer ran make dev/migrate/seed, got /healthz 200, /readyz 200,
  acme in the tenant list, and exactly one agent (security-agent) from the capability
  filter; re-ran seed twice for byte-identical "already exists, skipping" output. Crucially
  it verified the leg NO prior reviewer reached: after `docker compose stop postgres`
  (/readyz 503, /healthz still 200) and a restart, /readyz returned 200 ON THE FIRST POLL —
  the pool reconnects rather than staying poisoned. Graceful shutdown logged "shutdown
  signal received" then "control-plane stopped cleanly" and exited promptly. A grep of the
  whole session log for the DSN password found no match.
  It also confirmed Phase 0's config tests were NOT weakened to pass: the only diff to
  pre-existing tests is added t.Setenv lines for the new required field; every original
  assertion is untouched. That was my main worry about this task.
Task 9: reviewer ruling on the malformed-DSN question — validating it at connect time
  rather than config time is correct here, because main.go connects BEFORE
  ListenAndServe, so "connect time" and "startup time" are the same instant; the process
  still fails fast with a clear message before serving traffic. Accepted.
Task 9: minor (deferred): postgres.Open does not close the just-opened *sql.DB when
  PingContext fails. Harmless today (every caller exits immediately), but a real leak if
  Open is ever called in a retry loop. Carried.
Task 9: minor (deferred): Makefile passes the DSN as an inline env assignment, visible to
  `ps` on a shared host. Accepted local-dev tradeoff, recorded for completeness.
Task 9: complete (commits 3166d7e..833bf80, review clean) RUNNABLE SERVICE DONE.

## Post-review fix round (controller, commit 4faff38)

Held these back until the Task 9 gate passed, deliberately: they are production code in
the reviewed range, and editing them earlier would have had the reviewer grade my patch
instead of the implementer's work. Now closed:
  (1) pinger.go: ping bounded at 2s INSIDE Ping, not at the call site, so no future caller
      can omit it. /readyz is unauthenticated and polled freely; a hung-but-not-refusing
      database would otherwise park one pool slot per probe forever.
  (2) config.go: maxAllowedOpenConns=500 and maxAllowedConnLifetime=24h ceilings. Rationale
      recorded in code: PostgreSQL's default max_connections is 100, so a mistyped 5000 is
      not ignored — it starves other instances then fails far from the typo.
  (3) cmd/seed: refuses a non-loopback DSN host unless AGENTMESH_ALLOW_REMOTE_SEED=1.
      Verified behaviourally, not just by unit test: a prod-looking host is refused by
      name, and localhost passes the guard and proceeds to the connect attempt.
      The unparseable-DSN branch returns a FIXED message rather than echoing the URL,
      because a DSN carries a password.
  Tests added: loopback forms, the override, a refusal not echoing the password, the three
  out-of-range config cases (including the negative duration the reviewer flagged as
  missing), and "localhost.evil.example.com" — a host that merely LOOKS like loopback and
  must be refused, proving the check is on the whole hostname rather than a substring.
Task 9 fix round: NOTE — I tripped gosec G101 myself by naming a test constant `pw`, the
  exact trap I had warned every implementer about since Task 7. Renamed to dsnUserInfo with
  a comment explaining the cause. Logged because it shows the warning needs to be "do not
  name it after a credential, G101 matches the NAME" rather than a list of forbidden words.
Task 9 fix round gates at 4faff38: build, vet, lint "0 issues.", -short ./... all packages
  ok, -race on config/seed/api ok, tree clean.
Task 9 fix round: NOT fixed, with reasons — sslmode enforcement (would break the
  spec-mandated local Quick Start; the compose Postgres serves no TLS) and suspended-tenant
  enforcement (the spec defines the field at :74/:131 but NO semantics for it anywhere;
  implementing it would be inventing product behaviour). Both carried to phase close-out.

BASE for Task 10: 4faff38
Task 10: FINAL TASK — docs only (learning-guide.md, architecture.md, README.md, ADR 0008),
  no production code. Dispatch must carry the accumulated carry-forwards below, since this
  is the last chance to record them where a reader will find them.
Task 10: implementer DONE_WITH_CONCERNS (2 commits). Controller verified: build, vet, lint
  "0 issues.", -short clean, tree clean, NO leftover containers or control-plane processes
  (checked explicitly — this was the dispatch's second critical rule after the 8-hour
  exposure incident).
Task 10: Step 5 hard gate genuinely executed — Quick Start commands run in order against a
  real stack, output matching the docs, stack torn down immediately. Port 8080 was occupied
  by an unrelated container (healthbridge-api-1) so verification used :8090 WITHOUT
  changing the documented default — correct call; a local conflict must not alter project
  defaults.
Task 10: verified all 9 carried-forward gaps are present in docs/learning-guide.md's
  "Known Gaps Carried Into Later Phases" (:776), each naming its owning phase, and that
  README:131 and architecture.md:101 both link to it. My initial grep for "gRPC target"
  returned 0 only because the prose is Indonesian ("target gRPC") — the gap IS documented;
  recording that so the zero is not misread later as a missing item.
Task 10: Ruling: REJECT deviation 4. The implementer deferred the middleware-ordering
  explanation to the in-code comments rather than the learning guide. Verified the docs
  consequence directly: grep for WithRecovery/innermost across all three docs found ONE
  incidental mention (an interview question at :685) and no explanation anywhere. The
  in-code comment is genuinely good, but the learning guide is the teaching document and
  this was the single defect in Phase 1 that cost a full review cycle plus a fix round.
  Added as topic 8, "Middleware Order Is Load-Bearing" (commit 1bd4370), in the same
  nine-heading Phase-0 format as topics 1-7. Content covers what is actually
  counter-intuitive: Chain folds right so the first-written middleware is outermost;
  WithRecovery reads the id off its OWN request so nothing above WithRequestID can see it;
  a panic unwinds past WithLogging before it writes, so the requests most needing a log
  line get none. Also records why the fix guarded the write rather than only reordering,
  and the generalisable testing lesson (middleware_test.go assembles its own Chain, so it
  proved composability while saying nothing about NewRouter — test the assembly you ship).
  Held myself to the standard I set for implementers: every symbol, test name, and quoted
  failure string in the section was verified present before committing, and the named test
  was actually run.
Task 10: Ruling: ACCEPT deviations 1-3. Port 8090 for local verification (defaults
  untouched); the brief's grep invariants live under its Step 7 not Step 3 (my dispatch
  mislabelled them, not the implementer); and `command grep` to bypass this shell's
  ugrep alias, which does not prefix "./" to dot-rooted matches and so silently breaks
  `grep -v '^./...'` exclusion patterns. That last one is an ENVIRONMENT QUIRK worth
  carrying: any future invariant check in this repo that relies on ./-prefix exclusion
  must use `command grep`, or it will report false cleanliness.
Task 10: controller history fix — the "chore: complete Phase 1" tag commit had landed
  BEFORE my docs commit, so the marker no longer marked the end of the phase. Rebased it
  to the tip (b0f209e). Checked safety first: branch has NO upstream (never pushed) and no
  other ref contains the commit, so no shared history was rewritten. Verified the tree hash
  is byte-identical before and after (3f3e8ac...) — history order changed, content did not.
Task 10: complete.

## PHASE 1 COMPLETE — final whole-branch verification at b0f209e

Gates: go build, go vet, golangci-lint v2 "0 issues.", full `go test ./...` (container-
backed, not -short) all packages ok, 112 top-level tests + 153 subtests, 0 FAIL.
Layering: `go list -deps ./internal/api | grep -E 'gorm|pgx'` empty — the store-interface
seam ADR 0008 relies on is intact.
37 commits from merge base a0770a5. Tree clean. No stray containers or processes.

### What is NOT done, carried out of this phase deliberately
1. SPEC CONFORMANCE GAP: Agent.Endpoint is not validated as a gRPC target (spec:115
   requires it). Phase 3 dials these values. Highest-value item on this list.
2. TenantStatus has no defined semantics for `suspended` anywhere in the spec. Phase 5
   builds authorization on it, so decide its meaning first.
3. Capability lists uncapped (bounded only by the 1 MiB body limit) with per-row inserts
   in one transaction. Cap + CreateInBatches when the store is next touched.
4. postgres.Open leaks the *sql.DB if its ping fails — harmless today, real in a retry loop.
5. statusRecorder drops http.Flusher/Hijacker/io.ReaderFrom. Any future SSE/streaming
   endpoint needs forwarding methods or must bypass that middleware. FAILS SILENTLY.
6. Empty PATCH still issues an UPDATE, moving updated_at with no semantic change.
7. Repeated ?capability= keeps only the first value.
8. ServeMux matches the escaped path, so {slug}/{name} can capture %2F-decoded "/".
9. DB TLS not enforced; sslmode=disable locally since the compose Postgres serves no TLS.
10. Authentication/authorization entirely absent — Phase 5, per spec:448, surfaced in 3
    rounds of security review and stated plainly in the README's Security Model.
All ten are documented in docs/learning-guide.md "Known Gaps Carried Into Later Phases".

### Process observations worth carrying to Phase 2
- FIVE briefs contained defects: Task 6's prescribed middleware order (2 Important
  findings), Task 7's fake that its own test could not pass, Task 7's lax Update, Task 7's
  blind empty-patch test, Task 8's fake ignoring filter.Cursor. Pattern: briefs specify
  handler and test code carefully but treat TEST DOUBLES as scaffolding, so a double ends
  up encoding a contract the real store does not have. Phase 2 briefs should specify fakes
  against the real store's contract explicitly.
- Per-task review gates cannot catch spec drift. Each reviewer reads its own brief's
  Produces list; nobody walked the spec's validation table until I did it during a security
  triage, which is how item 1 above surfaced after eight green gates. Phase 2 needs an
  explicit spec-conformance pass that reads the SPEC against the code, not the brief.
- Implementer-reported counts were wrong 5 times running, then right twice — the fix was
  handing over the exact grep recipe, not asking for accuracy. Tooling beat exhortation.
- THREE reviewer dispatches died on API 503/429 rate limits. Each re-dispatch succeeded.
  One died holding the Docker stack up and left a credentialed Postgres exposed on the LAN
  for ~8 hours. Mitigations now standing: compose binds loopback, and I check for leftover
  containers after every failed Docker-using dispatch.
- gosec G101 matches identifier NAMES against string literals. It bit Task 7 and then bit
  me in the Task 9 fix round despite my own warning. The usable rule: never name a test
  constant after a credential.

## Appendix: skill-authoring test log (2026-10-17)

Written while creating `.claude/skills/agentmesh-phase-dev/SKILL.md`, per the
writing-skills Iron Law (no skill without a failing test first).

RED attempt 1 — INVALID, contaminated. Ran a "no guidance" agent against the
real repo, asking it to design a fake store + staleness test. It did NOT fail:
it produced injectable time, a nil-vs-stale distinction, and the three boundary
cases. Cause: it found and read
`docs/superpowers/specs/2026-10-17-agentmesh-phase2-design.md`, written minutes
earlier, and quoted its lines 280 (boundary testing), 302-303 (the fake-that-
leaves-zero warning) and 306 (injectable time). The guidance was present, just
in a different file than the skill. A baseline run inside a repo that already
documents the answer is not a baseline.
  Finding worth keeping: this is itself evidence that a well-written SPEC
  carries the lesson without a skill. The skill's value is therefore for work
  where the spec is not open — reviewing, dispatching, writing briefs — more
  than for an implementer who was handed a good spec.

RED attempt 2 — isolated. Copied only internal/ + go.mod/go.sum to /tmp/redtest,
no docs/, no .claude/, no spec, and re-ran the identical prompt. This measures
what the codebase alone teaches.

### RED attempt 2 result — GENUINE FAILURE, two specific defects

Isolated agent (only `internal/`, no docs, no spec, no skill) produced:

1. `LastHeartbeatAt time.Time` — a VALUE, not a pointer. "Never heartbeated"
   collapses into "stale", because `time.Time{}.Before(anyCutoff)` is true. The
   `unknown` vs `unhealthy` distinction Phase 1 deliberately introduced is lost
   silently: an agent that never proved itself alive is indistinguishable from
   one that died an hour ago.
2. **It reasoned its way OUT of boundary testing.** Verbatim: "The test uses
   wide margins (`/2` and `*4` of the window) instead of millisecond-precise
   boundaries ... because the test and the handler both call `time.Now()`
   independently — an exact boundary test would be flaky."
   That is the correct diagnosis of a real problem and the wrong fix. Without
   injectable time it chose weaker coverage rather than fixing the testability
   defect — and off-by-one at the threshold is precisely the bug that produces
   either flapping or never-flipping.
3. It filtered on the raw `a.Status` column, not a computed status, so a stale
   agent with a stored `healthy` would pass the filter.

### GREEN result

Agent with the skill produced: `*time.Time` (nil = never = `unknown`, distinct
from stale); `now func() time.Time` injected into the fake with exact
boundary cases (just inside / at / just outside); filtering via a shared
`EffectiveStatus` so the fake and the real SQL predicate cannot drift; and it
raised a genuine spec ambiguity for ruling rather than inventing an answer
(does "omit stale" apply to the UNFILTERED list? — my spec only covered the
filtered query; now closed in Success Criterion 3).

### Attribution limit — stated honestly

The GREEN agent read BOTH the skill and the Phase 2 spec; the baseline had
neither. So this demonstrates guidance-vs-no-guidance, NOT skill-vs-spec. I
cannot separate the skill's contribution from the spec's, and should not claim
to. What it does establish: the codebase alone does not teach these two
lessons, and the combination does.

### The baseline's one genuinely better idea — kept

It had the fake take a PRE-COMPUTED cutoff (`AgentFilter.HealthyAfter`) rather
than calling a clock itself, reasoning that the real store will receive a
cutoff in its `WHERE last_heartbeat_at >= ?` predicate, so the fake should be
a pure function of its inputs like every other filter field. That is a better
store-contract design than the GREEN's clock-on-the-fake, and it is folded
into the spec. A baseline run is not only a measurement; it can out-design the
guided run on a specific axis.
