# E1-14 / W2-14 — pub: expiry zero-ref transition characterization

Frozen base: main@718cf85f726f8ed4137c942f57d23e17bf5acd90 (#269 merged).

## Question

#266/#269 proved an existing durable repair blocks D before a candidate can
become COMMITTED. E1-14 asks a different question: when a runtime-created
`pub:` is the last real reference of an Office/CreateFile block and expires by
Cassandra TTL, which mechanism discovers the resulting zero-ref state? Does any
current runtime path create a GC candidate, reach D(P1), or does P1/K1 simply
stay retained without discovery?

## Static analysis (pre-runtime, main@718cf85f)

- `pub:` is written by `addPublishAttemptReferenceFn` → `DB.AddBlockReference`
  with a 35-day TTL and no tracker/projection. `up:` is written through
  `AddProvisionalBlockReferenceWithExpiry`, which also writes
  `gc_provisional_block_refs` + `_by_day`.
- Production candidate creators: scanner Phase 0
  (`promoteBlockIfUnreferenced`, reached only from `up:` expiry projections),
  worker fs_object reference removal (`enqueueZeroRefBlocks`) and explicit
  `Service.EnqueueBlock`. Phase 1 only re-enqueues existing candidates.
  None observes a `pub:` TTL expiry.
- Expected: final `pub:` expiry → zero refs → no candidate/queue/D → retained.

## Contract and plan

Real Office/CreateFile writer, real Cassandra/SILO, existing e19 GC-OFF
isolation. No CQL insert of `pub:`, no direct DELETE of a reference, no repair
removal, no harness `EnsureBlockGCCandidateExact`, no fabricated HEAD/P/D.

1. Five named legs, each with its own owned org/library fixture:
   `pub-live-control`, `up-expires-pub-remains`, `final-pub-expiry-no-repair`,
   `final-pub-expiry-with-repair`, `repair-renews-after-zero-ref`.
2. No-repair fixture: the writer is stopped by the existing
   `fileFromBlocksAfterStagedBarrier` seam, after durable `pub:` staging and
   before `queuePendingPublishedFileRepairs`. Result: real P1/K1, real `up:`,
   real `pub:`, no repair row, HEAD unchanged. In-process abrupt interruption,
   not an OS crash claim.
3. Repair fixture: existing E1-13 shape — writer stopped after exact-P
   authority before HEAD (durable repair), then a real competitor HEAD makes
   the attempt natively UNKNOWN. Real lease kept ahead of the shared e19
   repair daemon; only the dedicated sweep's eligibility time is advanced.
4. `up:` expiry: renew the exact real `up:` referrer through the productive
   `AddProvisionalBlockReferenceWithExpiry` with a seconds-scale deadline (it
   retracts the 48h projection in the same batch), await actual Cassandra TTL,
   run real Phase 0. Expected: `up:` gone, projection resolved, `pub:` present,
   no candidate.
5. Final `pub:` expiry: shorten the existing real referrer through
   `DB.AddBlockReference`, await actual TTL, require global EQ zero refs. Run
   real Phase 0, Phase 1 and the productive worker for the owned org.
   Measure candidate (canonical + projection), gc queue, delete lifecycle,
   recovery root, exact P1/K1 and HEAD/tree.
6. Scanner scope: Phase 0/1 listings are filtered to the owned org/block and
   their day cursors are kept in memory; all classification, liveness reads,
   candidate persistence and projection deletion run unmodified production
   code. Phase 1 uses an integration-only exported entry to the same body.
7. Repair legs: before any visit, after `pub:` expiry, require zero refs,
   durable repair, no visitor progress and no candidate. Then run the real
   repair sweep: native UNKNOWN, exact `pub:<repo:commit:fs>` renewal, no fs:,
   HEAD/tree/P1 unchanged, repair retained.
8. Causal mutation (disposable container only): make TTL-bound `pub:` writes
   go through the `up:` projection writer. The same final-expiry leg must then
   reach a productive Phase 0 candidate. Not a fix; not taken to D.
9. Independent teardown: metadata/repair/K1, expiry trackers/projections,
   pending fs owners, GC candidates/projections and queue. Completeness,
   filtered/unavailable negatives, race repeats, vets, standard Docker
   go-all-test, final source/docs audit and quota checks.
10. Commit/push and create reviewable PR. No merge, no GC activation.

## Out of scope

No pub: expiry table, migration, production scanner support for `pub:`,
backfill, scheduler, candidate reconciliation sweep, repair cleanup redesign,
W2-12 durable loser fix, W2-13 bounded discovery, Phase5/6, A1 or GC
activation. A confirmed P2 is registered, not fixed here.

## Disposition rules

- Expected result: W2-14 safety subset CLOSED-EVIDENCE / NOT-X1-RED for the
  measured schedule; W2-14 transition/convergence OPEN;
  ISSUE-GC-PUB-REF-ZERO-REF-01 P2 FOLLOW-UP (confirmed retention gap).
- If a runtime path reaches candidate→D(P1) and a surviving repair/operation
  later publishes P1: P1, THIS-PR, VERDICT RED, minimal fix.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.

## Harness (test code 19360c566)

`TestPubZeroRefTransition` runs in the existing e19 isolated child, with GC OFF
on every endpoint it addresses. Each leg owns a new org/library.

- No-repair fixture: `SetFileFromBlocksPublicationBarriersForTest` afterStaged
  panics in-process. Observed: exactly one `up:` and one `pub:<commit>`, both
  TTL-bound, no repair row, HEAD unchanged, exact P1 materialized, K1 bytes.
- Repair fixture: `e113RealFixture` (after-authority interruption, durable
  non-expiring repair, real blockless competitor HEAD), then the E1-13
  eligibility control (created_at −1h, real lease +1h). The remaining `pub:` is
  the writer-staged `pub:<commit>` of the repaired attempt.
- `up:` expiry: canonical tracker read at EQ; productive renewal moves the
  exact referrer to a ms-truncated +3s deadline. The test requires the 48h
  projection gone and the short projection present, awaits EQ absence of the
  ref after the deadline, then runs Phase 0 (listed=1, cleaned=1, projection
  removed). `pub:` remains, global EQ refs live, no candidate.
- Final `pub:` expiry: `AddBlockReference(..., ttl=2)` on the same referrer;
  awaits EQ absence; requires empty `ListBlockReferrers` and
  `BlockHasReferencesGlobal=false`. Before and after shortening, the test reads
  `gc_provisional_block_refs` for the `pub:` referrer. Current runtime has no
  such row; this is asserted after the GC checks.
- Discovery: owned-scope Phase 0 (`ScanExpiredProvisionalBlockRefsOnce`),
  Phase 1 (existing integration-tagged `ScanOrphanedBlocksOnce`) and the
  productive worker `ProcessOrgOnce` for the owned org. Listings are filtered to
  the owned block; day cursors live in memory, so shared cursors are untouched.
- Retained-state check after each step: no `gc_block_candidates` row, no
  `gc_queue` row in any of the 32 buckets of the owned org, no block
  `gc_pending_items` row, canonical block present without claim/handoff, no
  delete lifecycle, no S3 recovery root for the block in any bucket, exact P1
  unchanged, K1 bytes equal, no `fs:`. HEAD unchanged (no-repair), or the
  E1-13 `retained` predicate (repair: exact repair row with null TTL,
  HEAD/roots, loser tree, P1, bytes). Repair legs also require no visitor
  progress at zero refs, before and after GC.
- The production pending probe needs an exact P identity. A block with no
  candidate cannot supply one, and the probe correctly errors. The queue check
  therefore reads the owned org's partitions directly at EQ.
- `repair-renews-after-zero-ref`: exact-session/library visit and classifier
  observers, then the productive sweep at +2h eligibility. One visit, native
  `unknown`, sweep error `unknown; retain queued repair`. Afterwards the only
  reference is `pub:<repo:commit:fs>` with TTL ≤ 35d.
- Teardown for every fixture: E1-11 metadata/repair/K1, E1-12 expiry
  trackers/projections, E1-13 pending fs owners. E1-14 adds: zero candidate
  rows, candidate projections for any observed identity, zero queue/pending
  rows, and every moved `up:`/`pub:` projection coordinate absent.

## Results

First complete matrix (same test logic, before the race repeat): 5/5 PASS,
20 teardown verifications.

- `pub-live-control`: `up:` + `pub:` live, no repair, Phase 0 cleaned=0,
  candidate/queue/D absent, P1/K1 present.
- `up-expires-pub-remains`: `up:` retired by TTL; Phase 0 resolved its
  projection; `pub:`-only; no candidate.
- `final-pub-expiry-no-repair`: `pub:` (no tracker/projection) retired by TTL;
  global EQ refs=0; Phase 0 listed=0 cleaned=0; Phase 1 enqueued=0; worker
  n=0; candidate/queue/lifecycle/root absent; P1/K1 retained.
- `final-pub-expiry-with-repair`: same zero-ref state with a durable, unvisited
  repair. The candidate is still absent before any repair visit, so NO-D here
  comes from absent discovery, not from the repair guard.
- `repair-renews-after-zero-ref`: native UNKNOWN renews exactly
  `pub:<repo:commit:fs>` (TTL 3024000s observed); repair retained; no fs:,
  HEAD/tree/P1 unchanged; candidate absent.

Accepted Docker checks on 19360c566 (test file SHA-256
e4b407ae48a48481e2eed7dd50ab0dd8cb0317aa8ecd8df1483b08913e8db8ea, matching the
container copy):

- Race matrix `-race -count=3`: 15/15 named legs across three fresh isolated
  children PASS, 60 teardown verifications, 0 data races; 135.649s package,
  193s wall.
- Ordinary and `-tags integration` vets PASS.
- Three own negative controls PASS: filtered completeness, unavailable
  backend, filtered isolated child.
- Causal mutation PASS (`scripts/e114-pub-zero-ref-mutation.sh`, disposable
  container): TTL-bound `pub:` writes go through
  `AddProvisionalBlockReferenceWithExpiry`. The unchanged leg
  `final-pub-expiry-no-repair` then reaches
  `E1-14 DISCOVERED (Phase 0 after final pub: expiry)`: a productive
  `gc_block_candidates` row at the exact P1 key. `up:` behaviour is unchanged
  and all four teardown verifiers pass. The mutation stops at candidate
  observation and does not go to D. The first mutation attempt ran Phase 0
  before the moved projection's ms deadline: Cassandra TTL has second
  granularity. The harness now also awaits a tracked deadline when one exists.
  That attempt is not accepted evidence.

## Disposition

Measured runtime and source agree: when a `pub:` expires by TTL and leaves a
block at zero references, no current mechanism discovers it. The production
candidate creators are Phase 0 (`up:` projections), worker fs_object reference
removal and explicit `EnqueueBlock`. None of them observes `pub:` TTL expiry.
The mutation shows causally that the missing `pub:` expiry projection is the
reason the zero-ref state cannot be discovered.

- W2-14 safety subset: **CLOSED-EVIDENCE / NOT-X1-RED** for the measured
  Office/CreateFile single-block schedules. `pub:` TTL expiry by itself creates
  no candidate, so it cannot lead to D(P1).
- W2-14 transition/convergence: **OPEN**. A dead block stays in storage.
- `ISSUE-GC-PUB-REF-ZERO-REF-01`: **P2 FOLLOW-UP**, confirmed at runtime as a
  storage-retention / GC-discovery gap, not an incorrect-deletion path.

The no-repair fixture is a real reachable schedule. A writer that dies between
`pub:` staging and repair queueing leaves no repair, no `fs:` and no future
discovery. After the `up:` (2 days) and `pub:` (35 days) TTLs, block metadata,
mapping and K1 stay retained, and nothing reclaims them automatically. That is
the concrete retention impact.

Suppose another path creates a candidate for such a block, for example an
explicit zero-ref enqueue after an unrelated `fs:` removal in the same org. In
the no-repair schedule, D is then correct collection: the attempt never reached
HEAD and has no repair, so no surviving operation can publish it. With a
repair, the E1-11/E1-13 guard evidence applies; it is not re-proved here.

Frozen-ledger coverage: the original E1-14 attack row asks to "run scanner/G5 to
D; resume persisted repair". In current runtime the scanner cannot reach a
candidate through this transition. D(P1) is therefore not reachable this way
and was not executed. The repair resume ran as specified (native UNKNOWN
renewal). A future `pub:` projection would make this transition discoverable.
The E1-11/E1-13 guard would then become load-bearing here, so that change must
re-run this matrix together with the guard-omission control.

Not covered: other funnels (SeafHTTP, Sync, OnlyOffice, cross-repo,
UploadFile), multi-block files, encrypted libraries, multi-DC visibility,
concurrent cleanup, W2-12 durable loser authority, W2-13 bounded discovery,
Phase5/6, A1 and GC activation. No production code changed.

## Audit correction and final-source checks (ce77a70c1)

Scoped audit finding (P3 evidence precision): the mutation candidate was
described as being at the exact P1 key, but the test only logged the key and
never compared it. The failure message now reports an explicit `exactP1`
comparison, and the mutation script requires `exactP1=true`. Failure path
only; no assertion on the passing path changed. Final test file SHA-256
e0f29ad352fe5e2710affd89d5dd65c288ca077d0c1b8b340e3d69cf8688a9c6 matches
the container copy.

- Race matrix `-race -count=3`: 15/15 legs, 60 teardown verifications,
  0 data races; 143.678s package, 223s wall.
- Both vets PASS; three own negative controls PASS.
- Causal mutation PASS with `exactP1=true`; all teardown verifiers pass.

## Standard regression

- go-all-test on 19360c566 (before the failure-message-only correction):
  exit 0, 2026-10-07 16:38–16:58 local. `go test ./... -short` PASS,
  integration 907.239s (E1-14 child PASS, 5 E1-14 teardown verifications),
  API 20/20 suites, OIDC 25/25 tests. The 74 SKIP lines are the existing
  optional 3-DC/topology suites; their execution is not claimed.
- Post-suite: main and e19 `scripts/check-test-cleanup.sh` report
  `CLEANUP_STATUS: clean`. Default org quota_usage 0 on both, storage_quota
  2000000000, policy hard (unchanged).
- **Accepted** go-all-test on final ce77a70c1: exit 0, 2026-10-07
  17:06–17:24 local. Integration 828.637s (E1-14 isolated child PASS, 5 E1-14
  teardown verifications), API 20/20 suites, OIDC 25/25 tests; the same 74
  optional SKIPs, whose execution is not claimed. Afterwards both backends
  report `CLEANUP_STATUS: clean` with quota_usage 0, storage_quota 2000000000
  and policy hard unchanged. The earlier 19360c566 run is historical.

## Final scoped audit

- Source: no production code, schema, migration, scanner, worker, repair,
  TTL or GC-config change. The diff is one integration test file, gate
  wiring in `TestMain`/Compose, two scripts and docs. `ScanOrphanedBlocksOnce`
  was already an integration-tagged hook.
- Plan items 1–10: all executed except merge/activation, which are out of
  scope by design. The only plan deviation is the queue check: the production
  pending probe cannot name a P for a candidate-less block, so the check uses
  a direct owned-org read (documented above).
- Prohibitions held: no CQL insert of `pub:`, no direct reference DELETE, no
  repair removal, no harness candidate, no fabricated HEAD/P/D.
- Findings: one P3 evidence-precision item (exactP1), corrected. Two harness
  issues were corrected during development, before any acceptance: the queue
  probe and the mutation deadline wait. No unresolved introduced P0/P1/P2 in
  the measured scope.
- Remaining OPEN: W2-14 transition/convergence (P2 retention), W2-12/13
  overall, other funnels, E1/X1, GC activation.
