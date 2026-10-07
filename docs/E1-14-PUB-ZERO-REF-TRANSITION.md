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
