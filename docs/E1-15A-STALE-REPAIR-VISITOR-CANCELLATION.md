# E1-15A — stale repair visitor vs concurrent durable cancellation

Frozen base: main@3583d989d39fc6c0e2706b426dde1131c3dcb3a7 (#270 merged).

## Question

A repair visitor has already loaded repair R and classified it UNKNOWN. While
it is in flight, a legitimate actor durably removes R: the writer's real
known-loser cleanup. GC then commits D(P1). Can the stale visitor, once it
resumes, durably create liveness on P1 after D?

## Static analysis (main@3583d989d)

In `repairPublishedBlockReferenceRepairVisit`, the UNKNOWN path after
`repairAfterClassifyBarrier` runs two durable re-checks
(`publishedBlockReferenceRepairStillPending`): one in the visit, and one at
the start of `renewPublishedBlockReferenceRepairLivenessIfPending`. Only then
does it call `renewPublishedBlockReferenceRepairLivenessFn` →
`AddPublishAttemptReferences` → `AddBlockReference` at LOCAL_QUORUM. That
write checks no claim, fence or D. A post-renewal re-check that observes
"gone" deliberately does not remove the new `pub:<repo:commit:fs>`: since
`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01`, local absence is not
destructive authority, and the pin is left to its 35-day TTL.

Two windows:

- W-pre: cancellation after classify, before the re-checks. Expected to be
  stopped by the re-checks.
- W-post: cancellation after the last re-check observed R present, before
  the renewal write. Code reading predicts a post-D `pub:` in current runtime.
  This is a hypothesis to demonstrate, not a claim.

CreateFile retries on `ErrLibraryHeadConflict` (`retryLibraryHeadMutation`,
8 attempts, new `up:`/`pub:`/R per attempt). A definitive real loser therefore
needs a real competitor HEAD at every attempt, until the request fails.

## Classification (frozen before runtime)

RED: any durable `block_references` row for the block, created by the stale
visitor after exact COMMITTED D(P1) and present once that visit returns. This
follows the E1-11 ledger criterion "renewal/promotion creates durable P1
after D" and the X1 invariant ("durably add a P1 reference"). Practical harm
(a dead pin that keeps a future incarnation P2 over-retained, no reachable
HEAD) is recorded separately; it does not downgrade the RED. A transient
renewal write that the same visit withdraws before returning is not RED. It
is recorded, and a crash in that window is a documented residual.

## Plan

1. Office/CreateFile writer, real Cassandra/SILO, existing e19 GC-OFF
   isolation; each leg owns an org/library. No CQL insert/delete of refs, no
   repair removal by the harness, no harness candidate, no fabricated HEAD/P/D.
2. Writer attempt 1 is held in-process at `W2PublicationAfterAuthorityBarrier`,
   with durable R1, exact-P authority and no HEAD. R1 eligibility follows the
   E1-13 control: created_at −1h, real lease +1h ahead of the shared daemon;
   the dedicated sweep runs at +2h.
3. The productive sweep visits R1 and classifies natively UNKNOWN, then pauses
   at the existing after-classify observer (W-pre) or at a new
   integration-only observer immediately before the renewal write (W-post;
   no-op in normal builds).
4. Cancellation: release the writer. A real blockless competitor publishes
   HEAD before each of the 8 attempts; every attempt gets applied=false, runs
   the real known-loser cleanup and clears its own R. Require: request not
   201, 8 observed loser cleanups, no repair rows, no `pub:`, no `fs:`, loser
   commits unreachable.
5. Move every real `up:` with the productive renewal API to a common
   seconds-scale deadline and await actual TTL, giving global EQ zero refs.
   Owned-scope Phase 0 must create exactly one natural candidate at exact P1.
   Owned-scope Phase 1 enqueues it; the productive worker over the owned queue
   reaches exact COMMITTED (lifecycle PUBLISHED, orphan COMMITTED, root).
6. Resume the stale visitor and wait for the sweep to return. A query observer
   on the visitor's session records every write to the block's references.
7. Legs: `retained-control` (R kept; renewal written; writer then wins),
   `clear-before-recheck` (W-pre → D → resume), `clear-after-recheck`
   (W-post → D → resume), `fresh-sweep-after-clear` (new sweep after
   cancellation does not visit or renew).
8. Mutations (disposable container): M-pre neutralizes
   `publishedBlockReferenceRepairStillPending`, so the W-pre leg must observe
   a post-D renewal write. If W-post is RED and gets fixed, M-fix reverts that
   fix and must reproduce the W-post RED.
9. On RED: preserve the RED evidence, then make the smallest fix consistent
   with GONE-CHECK-XDC. After renewal, local absence may only retain; it must
   be escalated to an EACH_QUORUM read; confirmed global absence withdraws
   exactly the pins this renewal wrote; read failure keeps the pin (fail
   closed for liveness). Then GREEN, unit tests for the decider, regression.
10. Teardown: TERMINAL continuation of every COMMITTED root, E1-11 metadata/
    repair/K1, E1-12 expiry, E1-13 owners, E1-14 candidates/queue/moved
    projections, plus recovery roots. Completeness gate, negatives, race
    repeats, vets, standard go-all-test, cleanup/quota checks, scoped audit, PR.

## Out of scope

Sync/SeafHTTP/OnlyOffice/cross-repo matrices, bucket contention, Paxos-domain
redesign, bounded discovery, durable loser witness, `pub:` projection,
dead-row or owned-pub over-retention fixes, multi-DC measurement (single-DC
Docker only; the re-check reads at LOCAL_QUORUM, the GC guard at EACH_QUORUM),
Phase5/6, PRE-GC, A1, GC activation.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
