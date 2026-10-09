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

## Decision content (to be completed with the measured result)

1. Contract split: X1 physical invariant vs PRE-GC convergence.
2. Cancellation-authority matrix.
3. Settlement scope and the fs_id constraint.
4. Corrections to the E1-15C wording ("dead", "solely").
5. Why the next production PR is not small.

## Out of scope

Any change to `publish_repair.go`, Sync, writers, GC Phase 0–6, schema,
migrations, hot paths, automatic UNKNOWN deletion, GC activation. No severity
is lowered without explicit approval.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
