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
- a known loser or pre-HEAD rollback whose request-local cleanup deleted the
  c1 row (`CleanupFailedPublishAttempt`) and then failed to clear R: no c1
  row either (found during implementation);
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
- E1-11 `unknown-retained` and the E1-13 backlog fixture (found by the first
  standard run): both need a genuinely pending repair and built it with a
  competitor that moved HEAD, which now settles it. They now leave HEAD at
  the attempt's parent, the class that legitimately stays UNKNOWN.

RED: the new integration legs on unmodified main (UNKNOWN, retained, vetoed).
Gate: unit + vet, E1-15E race `-count=3`, the amended suites, gate negatives,
standard Docker go-all-test, cleanup, `GC_ENABLED=false`.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.

### RED (unmodified runtime, test 8d664b6bf)

`TestAbandonedRepairSettlement` against main's runtime: `parent-head-retains`
PASS; the other five legs FAIL because every sweep classifies R natively
UNKNOWN and retains it ("publication outcome ... is unknown; retain queued
repair").

### GREEN (fix e405f3027 and follow-ups)

- **Unit** (`go test -race ./internal/api/v2 ./internal/api ./internal/db`):
  PASS. That covers the witness walk table (9 cases), unwitnessed walks, the
  owned `pub:` then row order, HEAD = parent, a missing commit row,
  re-anchor, chunks, remove/delete failure with retry, and the HEAD-writer
  source guard (UPDATEs plus library-creation INSERTs). The existing
  head-observation budget test first caught an eager parent read; the read
  is now lazy, so a visit that does not walk reads no commit row.
- **Mutations** (`scripts/e115e-superseded-mutation.sh`): removing the
  A ≠ P guard, the owned-`pub:` removal, the no-renewal early return, or the
  witness itself each turns the named unit test RED.
- **Integration**, isolated e19 child: E1-15E 6/6. Amended suites: E1-15D
  2/2, E1-15C 4/4, E1-12 6/6, E1-11 6/6, E1-13 5/5. Measured: a natural
  D(P1) reaches exact COMMITTED after the settlement. Sync after the
  settlement, and Sync paused before its repair or before its CAS, publishes
  with its own `fs:` while Phase 0 makes no candidate. With Sync paused
  before its CAS, its own fresh repair (HEAD = its parent) stays UNKNOWN in
  the same sweep. A stale renewal is written and then withdrawn.
- **Race** `-count=3`: E1-15E 18/18 legs; 0 data races; 90 teardown
  verifications (303s wall). Gate negatives: 3/3 PASS.
- **Unchanged controls rerun**: the E1-11/E1-12/E1-13 omission mutations, the
  E1-15A mutations, the E1-15B finalizer negative, and the E1-15A–D gate
  negatives all PASS. This matters because `e115aCommitD` was split into
  reusable helpers with identical logs.
- **Standard Docker go-all-test**:
  - First run on 0d0dfe16c: exit 1. E1-11 `unknown-retained` and all E1-13
    legs failed, because their fixtures needed an UNKNOWN repair and built it
    with a competitor that moved HEAD. Fixed in 04907118d (see the amended
    evidence above). Not accepted.
  - Accepted run on 04907118d, 2026-10-09 13:01–13:32 local: exit 0.
    Integration 1587.426s (66% of 40m); API 20/20; OIDC 25/25; the same 75
    SKIPs. Afterwards both backends report `CLEANUP_STATUS: clean`.
- **Not run**: the 3-DC multi-DC suites (they need the 3-DC stack). By code
  reading, their resumable-classifier leg expects REACHABLE from the durable
  cursor, which the witness cannot change: the target is visited before its
  parent.

### Audit

The only production change is `publish_repair.go`; the `db` change is a
comment. Sync, writers, GC, schema and hot paths are untouched. The witness
was re-checked against:

- every HEAD write (three CAS UPDATEs plus one with no caller; three INSERTs
  that only create libraries under a fresh id);
- every producer of repair rows;
- the persisted commit graph, which is first-parent only (`second_parent_id`
  is not stored).

Two gaps found during implementation are now listed out of scope: the
known-loser cleanup deleting the commit row, and E1-11/E1-13 fixtures that
relied on the old behaviour. No unresolved introduced P0/P1/P2.
