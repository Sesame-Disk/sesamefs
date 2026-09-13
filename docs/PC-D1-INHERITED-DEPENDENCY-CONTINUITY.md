# PC-D1 - Inherited dependency continuity decision

**Status:** DECIDED architecture freeze; documentation and executable
characterization only.
**Issue:** `ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`
**Branch:** `docs/pc-d1-inherited-dependency-continuity`
**Baseline:** `main@2936c1179` (PC-1 merged)

This document is the source of record for the inherited-dependency decision
required before PC-2. It adds no publication runtime, schema, migration,
importer, funnel migration, GC behavior, or configuration change.
`GC_ENABLED=false` remains mandatory.

## 1. The gap is real

R3 defines the logical positive delta as:

```text
LogicalPositiveBlockDelta =
  UniqueCanonicalSHA256(ReachableBlocks(new HEAD))
  - UniqueCanonicalSHA256(ReachableBlocks(old HEAD))
```

For:

```text
H1: A B C
H2: A B C D
```

the delta is `{D}`. `A`, `B`, and `C` are inherited and are therefore omitted,
even if `B` was first published under `UNKNOWN` and `C` under `CONDITIONAL`
continuity. No later incremental publication revisits those proofs.

The executable test
`TestPCD1LogicalPositiveDeltaOmitsUncertifiedInheritedDependencies` uses this
exact vector and fails if an implementation silently treats the logical delta
as a complete work set.

This is a positive-continuity gap, not a claim that GC is already safe. The
existing executable Phase 5 characterization,
`TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD`, shows a
dangling expired commit can delete `fs_objects` still reachable from HEAD.
`ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` remains a separate P0 PRE-GC
fix, and is dormant only while `GC_ENABLED=false`.

## 2. Architecture comparison

| Dimension | A - coordinator-expanded | B - GC-owned | C - baseline certification |
|---|---|---|---|
| Hot path | O(reachable tree/dependencies) for every publish unless it grows durable per-dependency state (which becomes C) | No publication cost; destructive work pays the scan cost | O(reachable tree) only when the witness is absent/stale; steady state is O(newly-live) plus one witness predicate |
| Tree reads | Every publish, or every dependency-status lookup | Every destructive scan and every stale-item revalidation | One complete walk per certification; no inherited walk while the witness matches HEAD |
| Multi-DC | Every ordinary publish depends on all proof reads being available | Destructive GC must fail closed if a DC cannot answer reachability | Certification and witness writes use the canonical global/LWT domain; a remote failure blocks certification, not already-certified incremental publishes |
| Crash/restart | Requires durable per-dependency progress or repeats a full walk | Existing queue is durable, but a queued keep-set can become stale | Partial work grants no authority; no witness means retry. Existing durable pins/repairs remain the recovery state |
| HEAD advances during work | A stale full walk must be discarded and repeated | A stale destructive snapshot must be fenced before delete | A certification for H is accepted only with `IF head_commit_id = H`; a later legacy H->H' makes the witness invalid by equality |
| Existing libraries | Only the next mutation discovers the gap; inactive libraries remain unexamined | Cannot prove or repair historical publication continuity | Explicit backfill or lazy certification; an un-certifiable library is fail-closed |
| Libraries created after cutover | Full proof remains the default cost | Still depends on correct publication and GC | Empty initial HEAD may be certified atomically once the initializer is V-aware; otherwise first coordinated use certifies it |
| Content resurrection | Newly-live and still adapter-owned | Does not protect the resurrection publication race | Historical content is newly-live again and still requires BORROWED own pin and `ExpectedP` |
| Durable state | Per-dependency proof, or a baseline/frontier equivalent to C | Queue identity, reachability snapshot, and delete fences | Canonical witness `(library, certified HEAD, contract version)`; a cursor is optional optimization, never authority |

### Why A is not selected

Without a durable frontier, A converts a one-time historical problem into a
tree-sized hot-path tax. With a frontier, the coordinator is simply redoing the
certification state machine selected by C. A also leaves inactive libraries
without any new protection and does not repair a missing physical object.

### Why B is not the continuity owner

GC is a destructive authority. A sharing-aware GC is required to preserve every
object reachable from the current HEAD, but a negative answer from GC cannot
prove that an old publication had valid provenance, cannot reconstruct bytes
that are already missing, and cannot certify a library that was never checked.
Making GC the continuity owner would also couple publication availability to
the destructive worker and create an ambiguous coordinator/GC responsibility
boundary. B is therefore a mandatory PRE-GC safety repair, not the answer to the
PC-D1 positive-continuity question.

## 3. Decision: certified baseline frontier

```text
INHERITED CONTINUITY OWNER = CERTIFIED BASELINE FRONTIER
```

The frontier is a library-level continuity protocol:

1. Observe the canonical HEAD `H` for library `X`.
2. Walk the complete tree of `H`, canonicalize and deduplicate every block, and
   prove the continuity contract `V` for each physical dependency.
3. Commit a durable witness only if the canonical HEAD is still exactly `H`.
4. For a later coordinated publication `H -> H'`, prove only the
   `LogicalPositiveBlockDelta` and atomically advance both HEAD and the witness
   to `H'` under the same contract version.

This gives an induction proof: the baseline certifies all dependencies in H;
each subsequent coordinated publication certifies its positive delta; therefore
all dependencies in the current certified HEAD have a continuous chain of
proofs. If a witness is missing, stale, or from an unsupported version, the
incremental path is not admissible: certify the current HEAD first or fail
closed.

The coordinator is the only productive consumer and advancer of this frontier.
GC must preserve current-HEAD reachability, but it does not create, interpret,
or replace the witness. There is no double ownership.

## 4. Witness contract

The exact semantic statement is:

```text
library X
certified through HEAD H
under continuity contract V
```

The future canonical representation is two fields on the existing `libraries`
row (not a process-local map and not a second authority table):

```text
continuity_certified_head_commit_id = H
continuity_contract_version         = V
```

The witness is valid only when all of the following hold:

- the live canonical row is the same `(org_id, library_id)`;
- `head_commit_id == continuity_certified_head_commit_id == H`;
- `V` is the currently accepted contract version;
- the certificate's complete tree walk found every reachable `fs_object`;
- every canonical block has a valid physical `(storage_class, storage_key)`,
  readable bytes/metadata, no incompatible GC retirement authority, and a
  durable current-library liveness reference (or an idempotent repair that
  establishes it);
- any read error, unavailable DC, incomplete tree, missing object, ambiguous
  CAS, or unsupported version fails closed.

The certification write is conceptually:

```text
UPDATE libraries
SET continuity_certified_head_commit_id = H,
    continuity_contract_version = V
WHERE org_id = X.org AND library_id = X.id
IF head_commit_id = H
```

The coordinated advance is conceptually:

```text
UPDATE libraries
SET head_commit_id = H',
    continuity_certified_head_commit_id = H',
    continuity_contract_version = V
WHERE org_id = X.org AND library_id = X.id
IF head_commit_id = H
   AND continuity_certified_head_commit_id = H
   AND continuity_contract_version = V
```

These statements are target vocabulary for the later implementation; this PR
does not add the columns or execute either statement. Derived projections remain
secondary and cannot certify a HEAD.

### Moving-HEAD proof

```text
t0  certifier observes H
t1  another writer advances HEAD H -> H'
t2  stale certification attempts witness(H,V) with IF head_commit_id = H
    -> applied=false; no witness for H' is written
t3  a fresh certification must observe and prove H'
```

If the stale certification wins at `t1` and a legacy writer advances HEAD at
`t2`, the row contains `head=H'` and `certified_head=H`; equality fails, so the
witness is unusable. A crash before the final LWT also leaves no certification
authority. An ambiguous final LWT is settled by reading HEAD and both witness
fields in the canonical serial domain; inconclusive evidence retains the
uncommitted state.

## 5. Lifecycle and boundaries

### Existing libraries

Use an explicit backfill or lazy first-use certification. Backfill may be
parallelized, but only the final exact-HEAD LWT is authority. A library whose
tree or physical bytes cannot be proven remains uncertified and cannot take the
incremental publication path. Certification does not claim that an earlier
UNKNOWN/CONDITIONAL publication was historically safe; it establishes a new
safe baseline from the bytes that exist now.

### Libraries created after cutover

The empty initial HEAD can receive a V-aware witness in the same initialization
domain. Until the initializer is migrated, a newly-created library has no
implicit certificate and the first coordinated publication must perform the
baseline gate.

### Legacy writers during rollout

An old writer may advance HEAD without updating the witness. That is safe for
the decision: `head != certified_head` makes the witness stale. The next
coordinated writer must certify the new current HEAD; it may not infer a chain
from the old witness across the legacy write.

### Content resurrection

If a resurrected historical object is absent from the old HEAD, it is
newly-live and remains the responsibility of the BORROWED adapter: own pin,
exact `ExpectedP`, publication staging, repair, and fence. A baseline witness
must never bless it merely because the object appeared in an older commit.
The four current resurrection routes remain open W2 work.

### GC and activation

Phase 5 still needs a sharing-aware keep-set or a metadata-only retirement
change before any destructive activation. A valid witness is not a GC delete
authorization, and `GC_ENABLED=false` remains required for this PR and for the
current fleet.

## 6. PC-2 contract

```text
PC-2 may assume:
  - incremental newly-live evidence is sufficient only when the predecessor
    HEAD has a valid durable witness (X, H, V);
  - the witness is consumed and advanced atomically with the HEAD CAS;
  - inherited dependencies need not be rescanned while that equality holds.

PC-2 may NOT assume:
  - every existing or newly-created library is already certified;
  - a local read, in-memory flag, partial scan, or GC reachability observation
    is a witness;
  - current Phase 5 protects inherited dependencies;
  - content resurrection, W2, R31, G4, G5, or X1 is closed;
  - an unavailable or unprovable HEAD can be certified by inference.
```

Before the first productive funnel migration, a separate implementation
prerequisite must add the canonical witness state, certification/backfill gate,
and atomic HEAD+witness CAS. PC-D1 intentionally does not land that runtime
work.

## 7. Evidence and merge criteria

The PR must include:

- the inherited-delta counterexample and moving-HEAD witness model tests;
- a source-contract test that pins the single owner, the conditional meaning of
  `newly-live`, the exact witness rule, and the open issues;
- a Docker 3-DC probe using an ephemeral test-only table (no migration) that
  proves a stale witness CAS cannot certify `H'` after HEAD moved from `H`;
- a mutation script proving that removing the HEAD condition, claiming
  `newly-live` is unconditionally complete, or changing the owner makes tests
  RED;
- `go test ./... -short`, the PC-0/PC-1 contracts, the mutation suite,
  `go vet ./...`, and `git diff --check`, all run through Docker.

The issue is marked **decision resolved / implementation prerequisite open**.
W2, R31, X1, content resurrection, G4/G5, and
`ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` remain OPEN. No funnel is migrated,
no publication runtime changes, and no GC activation is permitted.
