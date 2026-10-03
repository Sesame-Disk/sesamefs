# E1-2: reachable-HEAD delayed repair

Base: main@c1f31f7ec, merged E1 slice #250, 2026-10-03.
Scope: one real Office CreateFile publication through the production shared
repair path, single-DC Cassandra 5.0.9 RF1 and pinned SILO
`RELEASE.2026-09-16T00-00-00Z`. No production runtime edits.

## Experiment and supported result

`TestE12ReachableHEADLateRepair` exercises the actual CreateFile handler.
An existing integration barrier stops it immediately after applied HEAD and
before permanent-reference promotion. The handler itself has materialized P1,
staged temporary references, queued the durable repair, written commit/tree,
validated exact placement and won HEAD. The test independently reads the
HEAD -> commit -> root -> file -> block chain and the exact repair identity.
The barrier models interrupted request execution; it is not an OS process kill.

The no-GC control runs the productive cold classifier and repair visitor using
a fresh Cassandra session, then retries after settlement. The delayed leg also
checks that the writer's actual up:/pub: references have positive Cassandra TTL,
shortens those exact rows to two seconds through `DB.AddBlockReference`, and
waits for real expiry with both local enumeration and the global reference read.
This is a time control, not a production 48h/35d elapsed-time test. No temporary
reference is deleted and no CQL write constructs HEAD, repair, P or D authority.
Expiry trackers retain their normal schedule; the test drives the actual GC
candidate/queue explicitly, without claiming scanner expiry discovery coverage.

With zero real references, the durable repair reports REPAIR_GUARD_ONLY.
The actual G5 worker consumes the exact candidate, acquires/releases its claim,
and postpones before PREPARED/COMMITTED. Assertions require candidate retention,
no active claim/handoff, no delete lifecycle, no recovery root, unchanged P1/K1
and the original reachable repair. The resumed visitor independently classifies
REACHABLE, installs the exact `fs:<repo>:<fsID>`, removes pub: and its repair row,
and leaves HEAD/tree and P1/K1 intact. A repeated visitor is a no-op.

## Why COMMITTED and TERMINAL are not forced

For this supported funnel the requested sequence stops at the pre-D gate:

1. `internal/api/v2/files.go` queues pending repairs before the final exact-P
   validation and applied HEAD; the stop occurs after HEAD, before promotion.
2. `internal/db/publication_liveness.go` reads the organization's complete
   32-bucket repair domain at EACH_QUORUM. The repair's staged block IDs survive
   reference TTL expiry. A positive repair returns REPAIR_GUARD_ONLY.
3. `internal/gc/worker.go` executes that global proof after the settled claim and
   postpones on REPAIR_GUARD_ONLY before publishing the destructive handoff.
4. Reachable settlement in `internal/api/v2/publish_repair.go` promotes permanent
   fs: before deleting repair authority. The global proof re-reads real refs
   after a negative repair scan, covering that transition.

Consequently neither D1 COMMITTED nor its descendant TERMINAL is reachable in
this measured delayed-repair leg. Deleting the repair or overriding the worker
would manufacture the missing precondition. Such a mutation is a negative test
of the gate, not evidence that current production code permits that sequence.
No post-D reachable repair execution is claimed as tested.

## Disposition and limits

PASS at the named pre-D gate is positive exclusion evidence for this Office
publication plus surviving durable repair. It does not close all of E1-02,
W2-11 or W2-14: late acquisition after the GC scan, renewals, concurrent cleanup,
other funnels, actual process restarts and material multi-DC failure legs still
need their own evidence. The matrix remains partial, X1 OPEN, and production GC
activation unchanged. Phase 5/6, stale-claim, clock-health, PRE-GC and A1 are
outside this PR.

## Validation — PASS, 2026-10-03

All executions were sequential in Docker against real Cassandra and pinned
SILO. The runner was built from main@c1f31f7ec plus the E1-2 test; only docs
changed after its final build.

- The standard Compose go-all-test command passed Go unit/integration, API and
  OIDC suites with its required evidence gates. Integration completed in
  474.042s; the new E1-2 test passed both named legs within that suite.
- `go vet ./...` and `go vet -tags integration ./...` passed.
- An isolated container-only mutation bypassed REPAIR_GUARD_ONLY immediately
  after the productive worker's global read. The real worker advanced and the
  new test was RED at `GC progressed while publication unresolved: n=1 err=<nil>`.
  This shows the assertion detects loss of the actual pre-D gate, rather than
  merely calling its classifier. The mutation is not a production counterexample.
- The original worker source was restored and compared before running
  `go test -tags integration -race -v -count=10 -timeout 5m ./internal/integration
  -run '^TestE12ReachableHEADLateRepair$'`. All ten repetitions passed in
  36.187s, with no race reports. Host production source was never mutated.
- Final diff and formatting checks passed. No production runtime files changed.

The reference clock control and request-interruption barrier are described
above. Optional isolated 3DC, proxy and saturation legs are not claimed as
passed. No post-D reachable-repair execution, matrix-wide closure or GC
activation follows from these results.
