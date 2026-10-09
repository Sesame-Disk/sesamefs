# E1-15D — post-D contract decision and repair cancellation authority

Frozen base: main@24372ba95fad (#273 merged).

## Purpose

E1-15A/B/C produced enough evidence to decide what X1 guarantees and what must
converge before GC activation. This PR records that decision as a proposal for
explicit approval and defines what authority a future repair settlement needs.
It changes no production code. One cheap evidence pair checks the premise
the authority depends on: can an abandoned v2 commit still be published?

## Static analysis (main@24372ba95)

- `publishedBlockReferenceRepairCommitDefinitelyNotReachable` exists, but the
  classifier deliberately has no emitter for it. Even if it were emitted,
  settlement would retain the row ("no durable cleanup authority").
- HEAD writers, all CAS on the expected HEAD at SERIAL:
  - v2 `UpdateLibraryHeadFromSnapshot`: new commit, parent = expected HEAD;
  - Sync `updateLibraryHeadWithStats`: target with parent = HEAD
    (fast-forward), or a fresh merge commit with parent = HEAD;
  - `InitializeLibraryHeadIfUnset`: only when HEAD is null;
  - `AdvanceLibraryCertifiedFrontier`: no production caller.

  Every write moves HEAD to a child of the current HEAD; commits are
  immutable (PutCommit write-once, #208). By code reading, HEAD never returns
  to an earlier commit.
- Publication paths for an existing commit c1 (an abandoned v2 attempt):
  1. The stale v2 writer's CAS `IF head = c1.parent` can apply while
     HEAD == c1.parent.
  2. Sync `UpdateBranch?head=c1`: fast-forward if c1.parent == HEAD, otherwise
     **auto-merge** when HEAD descends from c1.parent, which holds forever. The
     merge commit records only the current HEAD as its parent, not c1.
- Consequence (hypothesis to measure): the E1-15B/C repair is not provably
  dead. c1's content stays publishable through Sync. The E1-15C C2 veto of P2
  protects a still-publishable commit rather than pure dead retention.

## Evidence (one test, two legs, no production change)

Real Office/CreateFile writer subprocess, SIGKILL after repair queueing and
insertCommit (E1-15B schedule); real blockless competitor advances HEAD; real
in-process Sync `UpdateBranch?head=c1`, the existing W2 closure invocation:

- `sync-promotes-abandoned-commit`: no GC. Expected: Sync auto-merges c1's
  content into HEAD. Record status, the new HEAD, its parent, the published
  file and fs:, and the repair's later native classification.
- `sync-promote-after-terminal`: the same, but after a natural exact
  COMMITTED D(P1) completed to TERMINAL (E1-15A/B/C helpers). Expected: the
  existing Sync exact-P/fence path rejects; HEAD unchanged; P1 not
  reinstalled; K1 absent; no fs: for c1's fs_id.

Same prohibitions as E1-15A–C: no CQL insert/delete of references/repairs,
no harness candidate, no fabricated HEAD/P/D. Teardown, completeness gate,
gate negatives, race repeats, vets, standard go-all-test, cleanup/quota.

## Evidence results (test 24d88a6b0, no production change)

`TestAbandonedCommitPublishability`, e19 isolated child.

- **`sync-promotes-abandoned-commit` — confirmed.** The writer subprocess is
  SIGKILLed after queueing and insertCommit, leaving durable R (UNKNOWN) and
  a complete commit c1 with parent H0. A competitor advances HEAD to H1. Real
  Sync `UpdateBranch?head=c1` returns **200**, auto-merges and publishes
  HEAD M with parent = H1 (not c1). M's tree contains c1's file with c1's
  fs_id, whose `fs:` is now present on exact P1. **R was protecting a
  commit that could still be published.**
- **`sync-promote-after-terminal` — fail-closed.** Same, but after a natural
  exact COMMITTED D(P1) completed to TERMINAL: the promotion is rejected
  (HTTP 500, "failed to auto-merge sync head"). The test captures the
  handler log and requires the cause: "publication readiness check failed
  for auto-merged commit … block … is not currently reusable (… block delete
  is in progress)". HEAD stays H1; P1 not reinstalled; K1 absent; no `fs:`.
  The status mapping (500, not a retryable 409) is a minor UX observation;
  not changed here.

Accepted checks: matrix 2/2; race `-count=3` 6/6 (3 auto-merge publications,
3 gate-attributed rejections), 30 teardown verifications, 0 data races
(153.757s package, 263s wall); vets and three gate negatives PASS. Container
SHA-256 7e988a8bb98f9dfa1239921b42f4167218d8d5ab153957ca9a8045f3338f713f.

## Standard regression and final audit

- Integration budget raised 30m → 40m in both Compose runners: #273's
  accepted run used 87% of 30m, and E1-15D adds another mandatory isolated
  child. Time budget only.
- First standard go-all-test: exit 1. Two failures in tests this PR does
  not touch:
  - `TestKnownLoserCrashSafety/crash-pending`: "wrong definitive loser:
    (empty)". This is an E1-12 harness race: `e112AwaitFile` only stats the
    marker, and the parent read it between the child's create and write.
    Fixed in db1cb229a (test only): the reader now polls for non-empty
    content. E1-12 then passed 3/3 isolated.
  - `TestLibraryProjectionRegression_ReconcilePendingStorageCountersAfterSoftDelete`:
    a 5s async-projection timeout, in the known single-node projection flake
    class. It passed 3/3 isolated; not changed.
  Not accepted as evidence.
- **Accepted** go-all-test on db1cb229a: exit 0, 2026-10-08 20:44–21:17
  local. Integration 1736.896s, with the E1-15D child PASS: one auto-merge
  publication and one gate-attributed rejection. API 20/20; OIDC 25/25;
  the same 75 SKIPs. The run used 72% of the 40m budget; it would have used
  96% of the former 30m.
- Afterwards both backends report `CLEANUP_STATUS: clean`, with quota_usage 0
  and hard limits unchanged.
- Scoped audit. No production file changed. The diff is the E1-15D test,
  gate wiring, a gate-negatives script, the E1-12 marker-read fix, the budget
  change and docs. Prohibitions held. A first draft asserted only a non-200
  rejection; before acceptance it was strengthened to require the exact Sync
  readiness-gate cause from the handler log. One unpushed commit message was
  corrected to mention the Compose gate wiring. No unresolved introduced
  P0/P1/P2. No severity was changed: every disposition here is a proposal
  pending approval.

## Decision proposal (requires explicit approval)

### 1. Two contracts, stated separately

| Contract | Statement | Measured status |
| --- | --- | --- |
| **X1 — physical safety** | After exact COMMITTED D(P1), no operation may reinstall P1, revoke or revert its retirement, or publish HEAD with reachable content that depends on retired P1. | Office: no RED (E1-15C, plus E1-15D's after-TERMINAL rejection). Other funnels are not proven. |
| **PRE-GC — convergence** | Abandoned references and repairs must not indefinitely prevent reclaiming later lives of the same block (P2…), and must converge without weakening X1. | Not met: E1-15C (a repair vetoes D(P2)). |

The E1 ledger sentence "no current operation may durably add a P1 reference
after D" is not deleted. It is re-scoped as a convergence obligation, because
E1-15C/D show post-D references neither revive P1 nor bypass the publication
gate. E1-15A/B keep their P1 PRE-X1 labels until this split is approved. The
D0 §13 "post-D refs remain a contradiction/veto (TRANSITIONAL)" sentence must
be rewritten to match the code, which forbids post-COMMITTED revocation.

### 2. Cancellation-authority matrix for a repair R(c1)

| Situation | Authority today | Action |
| --- | --- | --- |
| c1 positively REACHABLE from HEAD | yes (existing classifier) | normal settlement (promote fs:, drop R) |
| writer observed applied=false | yes, but **process-local**: the request clears R synchronously | existing known-loser cleanup; a crash before cleanup loses the authority (E1-12) |
| c1 not found on HEAD's first-parent chain | **no** | retain. Sync can still auto-merge c1's content into a HEAD that never names c1 (E1-15D) |
| HEAD ≠ c1.parent (stale v2 CAS can no longer apply) | **partial**. With monotonic HEAD, the v2 writer can never win, but Sync promotion of c1 remains possible | retain |
| R UNKNOWN, old, or retries exhausted | **no**. Time is never authority | retain |
| ambiguous read / DC unavailable | no | fail closed (retain) |

`DefinitelyNotReachable` must not get an emitter until an authority source
exists. Today none does.

### 3. What a future cancellation needs (why the next PR is not small)

To retire R(c1) safely, c1 must become **unpublishable through every path**:

- (a) The stale v2 CAS is defeated once HEAD ≠ c1.parent. HEAD
  monotonicity, from the HEAD-writer audit above, makes that durable. No
  new state is needed.
- (b) Sync promotion of c1 (fast-forward and auto-merge) must be defeated
  **durably and atomically with that promotion**. This needs a commit-level
  cancellation that `handleSyncHeadPromotion`/auto-merge checks, for example
  an LWT-retired commit row or a cancellation marker read in the same
  serial domain as the HEAD CAS. That is new protocol: it touches Sync
  and possibly schema, and needs its own RED→GREEN.

Only after (a) and (b) may a settlement remove, exclusively:

- R's own repair-owned `pub:<repo:commit:fs>`;
- the R row.

It must never remove `fs:<repo:fs_id>`. fs_id is content-addressed, and
E1-15C/D measured a live publication sharing it. It must also never touch
another attempt's `pub:<commitID>`, any `up:`, other repairs, HEAD/commits, or
a later life P2.

### 4. Corrections to E1-15C wording

E1-15C described R as "dead" and the P2 veto as caused "solely" by a dead
repair. E1-15D shows c1 stays publishable through Sync while R exists. The
C2 veto therefore protects a commit that is abandoned but still publishable.
The retention harm is real, and its cure is cancellation (section 3), not
deleting R. The E1-15C and DEAD-ROW-RETENTION records are amended
accordingly. No severity changes.

### 5. Recommended sequence

1. Approve or amend sections 1–2. This is a contract decision; nothing is
   changed by this PR.
2. Next production PR: design plus RED→GREEN for commit-level cancellation
   (3b). That includes a characterization leg where Sync promotes c1 *after*
   a cancellation attempt.
3. Then the settlement of R under the exact removal scope of section 3.

## Out of scope

Any change to `publish_repair.go`, Sync, writers, GC Phase 0–6, schema,
migrations, hot paths, automatic UNKNOWN deletion, GC activation. No severity
is lowered without explicit approval.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
