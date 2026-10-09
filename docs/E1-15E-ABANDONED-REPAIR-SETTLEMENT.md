# E1-15E — minimal settlement of an abandoned publish repair

Frozen base: main@1b39d6a0b2f9 (#274 merged). Option A of
[E1-15D](./E1-15D-POST-D-CONTRACT-DECISION.md) §3.

## Problem (measured)

- E1-15C: an UNKNOWN repair R for an abandoned commit c1 vetoes D of every
  later life of its logical block (`RepairGuardOnly`), for as long as R exists.
- E1-15D: c1 can still be published by Sync auto-merge, and after that the
  classifier still says UNKNOWN, because the merge commit names the current
  HEAD and not c1.
- E1-12: a loser that crashes after `applied=false` but before its
  request-local cleanup leaves R with no settlement authority at all.

Nothing settles R today: `DefinitelyNotReachable` has no emitter and its
settlement retains.

## Authority: the superseded witness

For a repair row (repo, c1, fs):

1. P = `c1.parent`, read from the immutable commit row at `EACH_QUORUM`. A
   missing row, a read error, an empty parent or P = c1 gives no witness.
2. The resumable classifier's anchor A is one SERIAL observation of the
   canonical HEAD (existing). Require A ≠ P.
3. Walk the first-parent chain from A with the existing bounded, resumable,
   `EACH_QUORUM` walk. If c1 is visited, the outcome is REACHABLE (unchanged).
   If **P is visited before c1**, the outcome is the new **SUPERSEDED**.
   Genesis, the node bound, a timeout, a cycle or any read error stay UNKNOWN
   as today.

Why SUPERSEDED is permanent, not a snapshot:

- The chain is linear. If c1 were in it, it would be visited immediately
  before P.
- Every production HEAD writer installs a child of the current HEAD under a
  SERIAL CAS (`libraries` IF head = expected): v2
  `UpdateLibraryHeadFromSnapshot`, Sync `updateLibraryHeadWithStats` (fast
  forward requires target.parent = HEAD; merge commits take HEAD as parent),
  `InitializeLibraryHeadIfUnset` (null only). `AdvanceLibraryCertifiedFrontier`
  has no production caller. Commit rows are write-once (#208).
- So c1 can only become reachable through a CAS `IF head = P` that installs c1
  itself. At observation time HEAD = A ≠ P, and HEAD never returns to P
  (that would need P to be a child of a descendant of P). Every later
  `IF head = P` CAS fails.
- A CAS that installed c1 *before* the observation, even an ambiguous one,
  puts c1 on the chain from A: REACHABLE, not SUPERSEDED.

The witness is about commit c1, not about who queued the row. Every producer
queues `(repo, commit, fs)` for the commit it CASes or has already published:
v2 funnels (their new commit), Sync direct promotion (`targetHead` = the
commit itself; this is the shared identity), auto-merge (a fresh, unique merged
id), and Sync same-head repair (the current HEAD, which is REACHABLE). A
SUPERSEDED row therefore protects no attempt that can still publish c1,
whichever funnel wrote it. No funnel discrimination is needed or invented.

Sync can still publish c1's *content* through auto-merge. That publication
stages its own block delta, passes exact-P readiness, queues its own repair
under its unique merged commit and validates fences before its HEAD CAS
(E1-15D §3, W2-4 autoMerge legs). It never relies on R.

## Settlement (the only production change)

In `internal/api/v2/publish_repair.go`:

- Emit SUPERSEDED only from the resumable classifier (both the anchored walk
  and the post-genesis re-anchor walk). The non-resumable classifier (used by
  the stale-owner sweep) and `DefinitelyNotReachable` are unchanged: still no
  emitter for the latter, and it still retains.
- On SUPERSEDED, remove R's own `pub:<repo:c1:fs>`, then delete the R row.
  Do not promote `fs:`, do not renew, do not touch `fs:<repo:fs_id>`, another
  attempt's `pub:<commit>`, `up:`, owners, fs_objects, commits, HEAD or other
  repairs. Do not reuse `CleanupFailedPublishAttempt`.
- A failed removal or delete returns an error: the row stays and the next
  visit re-derives the same witness. Every crash point leaves the row (which
  keeps the GC veto) or nothing; never under-retention.
- A stale visitor that renews after the settlement is withdrawn by the
  existing E1-15A post-renewal global-absence check. If that check is
  unavailable, the residue is the known TTL-bounded
  `ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01` class.

A source guard pins the production HEAD-writer set, so a new writer forces a
re-audit of this witness.

## Out of scope

Sync, writers, hot paths, GC Phase 0–6, schema and migrations, global commit
cancellation, deletion by age, GC activation, X1 closure. Not covered by this
witness, and left UNKNOWN (retained):

- HEAD = c1.parent: c1 can still be fast-forwarded;
- a crash between repair queueing and `insertCommit` (CreateFile queues
  before inserting the commit): no c1 row, so no parent;
- distance from A to P beyond what the resumable walk completes;
- the stale pending-owner sweep (non-resumable classifier) and the writer's
  own TTL-bound `pub:<c1>` (E1-14 class).

The general retention issue is therefore narrowed, not closed.

## Evidence plan

Unit (TDD, `publish_repair_test.go`): witness at A ≠ P; no witness at A = P;
REACHABLE when c1 precedes P; missing or empty parent; parent read error;
self-parent; witness on a resumed cursor and on the re-anchor walk; the
non-resumable classifier never emits it; settlement removes the owned `pub:`
then the row, without promote or renew; failed removal or delete retains,
and a retry settles; `DefinitelyNotReachable` still retains; the HEAD-writer
source guard.

Integration (`TestAbandonedRepairSettlement`, isolated e19 child, real Office
writer SIGKILLed after queue + insertCommit, real competitor, real sweeps, no
CQL fabrication of HEAD, refs or repairs):

| Leg | Checks |
| --- | --- |
| `parent-head-retains` | HEAD = P: native UNKNOWN, R retained, only R's `pub:` renewed |
| `superseded-unblocks-d` | HEAD advanced: native SUPERSEDED; R row and owned `pub:` gone, nothing else written; no `fs:`; P1 intact; then real refs retired by TTL and a natural D(P1) reaches exact COMMITTED (no `RepairGuardOnly` veto) |
| `sync-after-settlement` | settle, then real Sync `UpdateBranch?head=c1` publishes c1's content with its own `fs:`; no repair rows left; bytes intact |
| `sync-paused-before-repair` | Sync paused after its staging and readiness (`W2PublicationBeforeRepair`); settle; retire the writer's refs; global refs stay non-zero and Phase 0 makes no candidate; resume: 200, own `fs:`, bytes intact |
| `sync-paused-before-cas` | the same at `W2PublicationAfterAuthority` (own repair queued, before HEAD CAS) |
| `stale-visitor-renew` | visitor paused after its last re-check (HEAD = P, UNKNOWN); HEAD advances; a second sweep settles; resume: the renewal is withdrawn by the global-absence check; final: no row, no owned `pub:` |

Amended existing evidence, because its subject now converges:

- E1-15D `sync-promotes-abandoned-commit`: the post-merge sweep now settles R
  (SUPERSEDED); the Sync `fs:` stays.
- E1-15C C1 `p2-published-with-dead-repair`: the rematerialization advances
  HEAD, so R settles; the shared legitimate `fs:` and P2 stay.
- E1-12 `crash-restart`: a new-process sweep now settles the crashed loser's
  R (the authority lost by the crash is recovered from the canonical chain).

RED: the new integration legs on unmodified main (UNKNOWN, retained, vetoed).
Gate: unit + vet, E1-15E race `-count=3`, the amended suites, gate negatives,
standard Docker go-all-test, cleanup, `GC_ENABLED=false`.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
