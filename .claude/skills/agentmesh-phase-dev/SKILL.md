---
name: agentmesh-phase-dev
description: Use when implementing or reviewing any AgentMesh phase task, writing a task brief, dispatching an implementer or reviewer, or writing a test double for a store interface in this repo
---

# AgentMesh Phase Development

## Overview

AgentMesh ships in numbered phases. Each phase gets a design spec, a task
breakdown, and a code review gate between every task. Phase 1 ran ten tasks
this way and the gates earned their cost — they caught two Important findings
the per-task tests missed.

This skill encodes what Phase 1 cost to learn. Every rule below traces to a
specific documented failure in
`docs/superpowers/retrospectives/2026-10-17-phase1-ledger.md`. That ledger is
the evidence; this is the summary.

**Core principle:** the test double is production code for the test suite. Five
of Phase 1's defects lived in test doubles, not in handlers.

## The Five-Time Failure: Test Doubles That Lie

**This is the single highest-yield rule in this skill.** It fired five times in
one phase, and in four of those the task brief's *own* prescribed fake could not
satisfy the brief's *own* test.

What happened, each time:

| Instance | The divergence | What it hid |
|---|---|---|
| Task 7 | Fake `Create` didn't stamp `CreatedAt`/`UpdatedAt`; the real store does | The brief's own create test could not pass |
| Task 7 | Fake `Update` wrote the whole struct; real store has a 3-column allowlist | Nothing today — but the fake was laxer than production |
| Task 7 | Fake didn't stamp `UpdatedAt` on update | `TestPatchEmptyBodyChangesNothing` was blind to the real `updated_at` bump its name claimed to check |
| Task 8 | Fake `List` ignored `filter.Cursor` | The brief's own malformed-cursor subtest could not pass |
| Task 8 | Fake `List` ignored `filter.Limit`, always returned `next=""` | Pagination plumbing had **zero** coverage; `Limit` and `next_cursor` could break entirely and stay green |

### A fake for a store interface IS

State these as the fake's contract, in this order:

1. **Every filter field the real store honours.** If `AgentFilter` has
   `Limit`, `Cursor`, `Status`, and `Capability`, the fake honours all four
   or the tests using it prove nothing about the ones it skips.
2. **Every field the real store writes through the pointer.** The real
   `TenantStore.Create` sets `CreatedAt` and `UpdatedAt` on `*domain.Tenant`
   before insert. A fake that skips this makes every timestamp assertion
   meaningless.
3. **The real store's error sentinels, from the real conditions.** Not
   `errors.New("nope")` — `domain.ErrNotFound`, `domain.ErrAlreadyExists`,
   `domain.ErrInvalid`, returned where the real store returns them.
4. **Deterministic ordering.** The real store orders by a keyset
   `(created_at, id)`. A fake iterating a Go map has no order; sort before
   returning, or every pagination test flakes.

### Two failures measured in an isolated trial

A fresh agent given only this codebase — no spec, no skill — was asked to
design a fake plus a staleness test. It failed in two ways worth naming,
because neither is carelessness; both are reasonable-looking choices:

**It used a value where a pointer carries meaning.** `LastHeartbeatAt
time.Time` instead of `*time.Time`. The zero value is "before" any cutoff, so
"never heartbeated" silently collapses into "stale" — losing a distinction the
domain deliberately encodes (`unknown` is not `unhealthy`). When a field has
a meaningful absent state, make absence representable; a zero value that
happens to sort correctly is not the same as modelling the absence.

**It reasoned its way out of boundary testing.** Verbatim: "an exact boundary
test would be flaky, because the test and the handler both call `time.Now()`
independently," so it used margins of half and four times the window instead.
The diagnosis was right and the fix was wrong. Flakiness at a boundary is the
untestable design talking; weakening the test to accommodate it is how
off-by-one ships. **If a boundary test looks flaky, fix the clock, not the
test.**

The same trial also out-designed the guided run on one axis — it had the store
take a pre-computed cutoff rather than calling a clock, which is the better
contract. Worth remembering that a baseline is not only a measurement.

### Before you finish a fake, read the real implementation

Open the real store method next to your fake method. Not the interface — the
implementation. Ask of each line that touches state: does my fake do this?

That check is what Task 8's cursor fix came from, and it is the check that was
skipped the five times above.

## Writing a Task Brief

A brief that prescribes exact code carries exact bugs. Phase 1's briefs were
written before any of the code existed, which is the honest reason — but the
consequence was that **five briefs shipped defects**, including the one that
prescribed a middleware order a reviewer later proved broken.

So: the brief's authorship does not grade its own work.

- **Specify fakes against the real store's contract**, explicitly, not as
  scaffolding. Name the filter fields the fake must honour and the fields it
  must stamp. This is the single change most likely to prevent a repeat.
- **Prefer naming the behaviour over pasting the implementation.** A brief
  that says "Create must roll back in one transaction, leaving no orphan agent
  row, and the test must assert the absent row rather than just the error"
  survives contact with reality better than thirty lines of prescribed Go.
- **If you prescribe code, mark it as a starting point**, not as verbatim text
  to reproduce. An implementer told "verbatim" will faithfully reproduce a bug
  and report DONE.

## Dispatching an Implementer

The dispatch prompt IS:

1. **The brief path**, read in full.
2. **Overrides**, where a later decision supersedes the brief, with the reason
   and the evidence. Phase 1's Task 8 brief wired `WithRecovery` outermost;
   the dispatch overrode it to innermost and said why, and the implementer
   applied it correctly.
3. **Carried constraints** from completed tasks that this task can regress.
   In this repo: `internal/api` imports no `gorm` or `pgx`; `WithRecovery` is
   innermost; `/healthz` must not touch the database.
4. **The TDD requirement**: tests first, RED output captured showing failure
   for the right reason (undefined symbols, not a logic error in the test),
   then GREEN.
5. **The gate list**, as exact commands to run and paste.
6. **The counting recipe**, verbatim (below).
7. **A demand for every deviation**, with reasoning, however small.

### The counting recipe — paste it, don't ask for accuracy

Five implementers in a row misreported their own test counts. Asking for care
did nothing. Handing over the exact command fixed it on the next two tasks.

```
top-level: grep '^=== RUN   Test' <file> | grep -vc '/'
subtests:  grep '^=== RUN   Test' <file> | grep -c '/'
```

Tooling beat exhortation. This generalises: when a report is wrong the same
way repeatedly, give the command, not the reminder.

## Verification Gates

Run on the task's final commit. Paste exact output.

```bash
go build ./...
go vet ./...
go test ./... -v          # or -short where a container isn't needed
go test -race ./<touched packages>/...
/tmp/golangci-v2-install/golangci-lint run ./...   # must print "0 issues."
go list -deps ./internal/api | grep -E 'gorm|pgx'  # must be EMPTY
```

**The lint binary is not the one on PATH.** `~/go/bin/golangci-lint` is v1 and
refuses this repo's `version: "2"` config, so the gate silently does not run as
written. Use the v2 binary in `/tmp`, and reinstall it when `/tmp` is cleared:

```bash
GOBIN=/tmp/golangci-v2-install go install \
  github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0
```

**gosec G101 matches identifier NAMES against string literals.** It fired on a
test constant named `secret` holding a fake database error, and again on one
named `pw` holding a fake password — the second time against an author who had
written the warning. The usable rule: never name a test constant after a
credential. Not a forbidden-word list; the matcher looks at the name.

## The Review Gate

One reviewer per task, over that task's commit range. It fixes nothing — it
reports.

The review prompt IS:

1. **The commit range and a saved diff path.**
2. **The binding authorities**: the spec (binding), the brief (not
   self-grading), the ledger's entries for this task.
3. **An explicit "already verified, do not redo" list.** Gates you ran
   yourself, rulings already made. This is what buys depth on the things that
   actually need judgement.
4. **A priority-ordered examination list**, because dispatches die mid-run.
5. **Questions to RULE on**, not restate. "Does the spec require X? If yes,
   finding. If no, say so plainly and assess acceptability."
6. **A mandatory verdict**: Critical/Important/Minor counts including zero,
   spec compliant yes/no, Approved or Needs Fixes.
7. **Leave-the-tree-clean instruction**, verified with `git status --short`.

A reviewer that verifies by *reproducing* beats one that re-reads. Phase 1's
best reviews reordered the middleware to confirm a test caught it, probed a
closed port to check a password couldn't leak, and reverted a fix to prove the
new tests were regression tests rather than proof of a fixed bug.

### Important findings get fixed, not parked

When a finding is Important, fix it in a follow-up commit before the next task.
Two of Phase 1's were coverage gaps where the shipped behaviour was already
correct — what was missing was any test that would notice it changing. Those
still got fixed, because "correct today, unguarded tomorrow" is how the
middleware bug would have come back.

**Hold security-review fixes until the task's review gate passes.** Editing
production code in a range awaiting review means the reviewer grades your patch
instead of the implementer's work.

## Spec Conformance Is Not Covered By Task Gates

**Phase 1's worst defect survived eight green gates.** The spec's validation
table requires `Agent.Endpoint` to "parse as a gRPC target"; the code checks
non-empty and ≤512 characters. Nobody noticed, because every task reviewer read
its own brief's Produces list and no one walked the spec's table row by row.

It surfaced only during an unrelated security triage.

So a phase is not complete until someone reads **the spec** against **the
code** — specifically every row of every table in the spec that states a rule.
Schedule it as its own pass. Task gates structurally cannot catch this: they
verify the brief was implemented, not that the brief covered the spec.

## Docker and Dispatch Deaths

Three reviewer dispatches died on API rate limits mid-run. Each re-dispatch
succeeded, so the deaths cost time, not correctness — **except one died holding
the Docker stack up and left a credentialed Postgres exposed on the LAN for
eight hours.**

A subagent killed mid-run cannot execute its own cleanup step. Therefore:

- Tell a Docker-using dispatch to tear down **the instant** its stack-dependent
  item finishes, not at the end of its work.
- After **any** failed dispatch that used Docker, check for leftovers yourself:
  `docker ps -a --filter name=agentmesh`.
- The compose file binds `127.0.0.1:5432`, not `0.0.0.0`. Keep it that way;
  that binding is what reduces a forgotten teardown from LAN exposure to a
  host-local annoyance.

## Security Findings: Triage Against The Spec

Automated security review will flag Phase 1's documented, deliberate
unauthenticated state every time — it did across three rounds, 11 findings in
one round alone. Most were out of scope by explicit spec ruling.

Triage shape:

- **Out of scope by spec ruling** — cite the spec line. Phase 1's spec line 448
  says every endpoint is unauthenticated until Phase 5. A finding asking for
  authentication is answered by that line, not by argument.
- **Misreads the code** — verify before dismissing. One finding claimed a fake
  hid a mass-assignment control; the real store's three-column allowlist meant
  no exploit path existed. Another claimed a DSN password leaked into an error;
  a probe against a closed port showed pgx redacts it itself.
- **Real and in scope** — fix it.
- **Real but contradicts the spec's own requirements** — say so. Forbidding
  `sslmode=disable` would make the spec-mandated local Quick Start impossible,
  because the compose Postgres serves no TLS.

**Record the ruling with its reason.** A dismissal without a cited spec line is
indistinguishable from an oversight six months later.

And when triage turns up something real that the finding only gestured at,
follow it: the `Endpoint` spec gap surfaced from an SSRF finding whose own
framing didn't apply.

## Red Flags

Each of these happened. Each means stop.

- **"The brief says verbatim, so I'll reproduce it exactly"** — including the
  bug. Five briefs had defects.
- **"The fake is just test scaffolding"** — it is production code for your test
  suite. Five defects lived there.
- **"All tests pass, so the wiring is right"** — Phase 1's entire suite stayed
  green with the middleware order reverted to the broken one. Tests that build
  their own chain prove composability, not that the assembly you ship is
  correct. Test the assembly you ship.
- **"I'll report the count from memory"** — wrong five times running.
- **"Eight gates passed, so it matches the spec"** — they verify briefs.
- **"The reviewer approved, so I'm done"** — Important findings get fixed.
- **"It's only a local dev credential"** — true, and it was still reachable
  from the LAN for eight hours.
- **"I'll name this test constant `secret`"** — G101 matches the name.
- **"An exact boundary test would be flaky here"** — correct, and it means the
  clock needs injecting. Measured: an agent hit this and chose weaker coverage.
- **"The zero value works as a sentinel"** — only if absence has no distinct
  meaning. Measured: it collapsed `unknown` into `unhealthy`.

## Ledger Discipline

Keep a running ledger per phase (Phase 1's is preserved at
`docs/superpowers/retrospectives/2026-10-17-phase1-ledger.md`, 1024 lines). It
records, per task: the base commit, the dispatch, rulings with their reasoning
and cost-if-wrong, review outcomes, and every deferred minor.

**Record your own corrections too.** Phase 1's ledger contains an entry where
the controller ruled a leak was live, probed it, found it latent, and corrected
the ruling in writing. A wrong ruling left standing is worse than the original
finding, because the next reader trusts it.

The ledger is why this skill could be written at all.
