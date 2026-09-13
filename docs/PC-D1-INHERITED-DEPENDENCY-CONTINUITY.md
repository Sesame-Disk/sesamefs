# PC-D1 - Inherited dependency continuity decision

**Status:** DECIDED architecture freeze; documentation and executable
characterization only.
**Issue:** `ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`
**Branch:** `docs/pc-d1-inherited-dependency-continuity`
**Decision development baseline:** `main@2936c1179` (PC-1 merged)
**PR merge baseline:** `main@33a41f822` (#217 merged)

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
| Multi-DC | Every ordinary publish depends on all proof reads being available | Destructive GC must fail closed if a DC cannot answer reachability | Certification and witness writes use one compatible global `SERIAL`/LWT domain; a remote failure blocks certification. A valid witness removes inherited-tree reads, but incremental publication still retains the availability requirements of newly-live proofs, readiness/fences, and the HEAD+witness CAS |
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
   run the following **GC-aware baseline handshake for every physical
   dependency** before counting that dependency as certified:

   ```text
   resolve/capture exact physical incarnation P
     → establish durable library-owned liveness
     → revalidate exact P + GC authority
   ```

   Here `P` is the exact physical incarnation and canonical placement tuple
   `(storage_class, storage_key)`, matching the GC/R26 vocabulary. PC-D1 does
   not invent a second incarnation field: a minted locator's UUID suffix is
   part of `storage_key` and therefore part of P. A legacy deterministic
   locator is not evidence of a fresh physical generation. Before such a
   dependency can enter a witness, the implementation must safely
   rematerialize/migrate it to a minted, never-reused P; until that transition
   is implemented and validated, a legacy deterministic dependency is
   un-certifiable and baseline certification fails closed. Any future
   `physical_generation` token would be a separate pre-PC-2 decision, not an
   implicit PC-D1 field. “Durable” means persisted in the canonical Cassandra
   domain and visible to the GC authority, not a process-local flag or an
   eventual write. A bounded-TTL `up:`/`pub:` pin may bridge the scan and
   handshake, but it can never by itself justify the witness. Before writing
   the witness, every dependency must have non-expiring current-library
   liveness (normally finalized `fs:` authority retained while the certified
   HEAD reaches it), or a confirmed idempotent repair that established that
   permanent authority. The lifetime must cover the witness/reachability
   frontier; a renewal failure fails certification. The final revalidation is
   a fresh authority check **after** liveness is established; it is not
   satisfied by the initial placement read or by a bare fence read.

   If any step is missing, ambiguous, unavailable, observes a changed `P`, or
   finds that GC already owns destructive authority, the
   dependency and the whole baseline certification fail closed. A late
   liveness write cannot revoke a zero-proof/GC authority already won, which is
   why the post-liveness revalidation is mandatory.
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
- every canonical block completed the baseline handshake above: the exact
  physical incarnation P = `(storage_class, storage_key)` was captured,
  non-expiring current-library liveness (or an idempotent repair that
  establishes that permanent authority) was persisted and visible to GC, and
  a fresh exact-P/GC-authority revalidation accepted that same P afterwards;
  a bounded-TTL pin is only a certification bridge and is never sufficient by
  itself;
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

### Canonical SERIAL domain prerequisite

Before frontier activation or PC-2, **all** canonical `HEAD` writers that
coexist with the frontier (legacy advances, initializers, rollback guards), the
baseline-certification LWT, and the combined HEAD+witness advance must use one
compatible global `SERIAL` Paxos domain. A supported multi-DC deployment must
reject `LOCAL_SERIAL` for this protocol until
`ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01` is closed; a warning is not a substitute
for the invariant. PC-D1 records this prerequisite but does not change runtime
configuration or pin existing statements.

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
uncommitted state. This invalidation guarantee depends on the global `SERIAL`
domain prerequisite above; `LOCAL_SERIAL` cannot provide one global frontier.

### GC-authority interleaving (mandatory baseline rule)

The HEAD conditional is necessary but not sufficient. The baseline must not
use this unsafe order:

```text
resolve P → observe GC zero-proof → write library liveness → certify
```

The zero-proof can already have granted destructive authority before the late
liveness appears, and that write does not retroactively revoke the authority.
The only admissible order for each dependency is:

```text
resolve/capture exact physical incarnation P
  → establish durable library-owned liveness
  → revalidate exact P + GC authority
  → include in the baseline certificate
```

The revalidation must use the same canonical authority domain as the relevant
GC fence/claim (and fail closed on an unavailable or ambiguous observation).
Only after **all** dependencies pass this handshake may the certifier attempt
the final `IF head_commit_id = H` witness write. The HEAD CAS protects the
logical frontier; the per-dependency handshake protects the physical
incarnation against GC.

## 5. Lifecycle and boundaries

### Existing libraries

Use an explicit backfill or lazy first-use certification. Backfill may be
parallelized, but only the final exact-HEAD LWT is authority. Backfill may
certify minted physical locators; a legacy deterministic locator must first be
safely rematerialized/migrated to a minted, never-reused P. A library whose
tree, physical bytes, or required permanent liveness cannot be proven remains
uncertified and cannot take the incremental publication path. Certification
does not claim that an earlier UNKNOWN/CONDITIONAL publication was historically
safe; it establishes a new safe baseline from the bytes that exist now.

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
    HEAD has a valid durable witness (X, H, V) backed by non-expiring
    current-library liveness for every inherited dependency;
  - the witness is consumed and advanced atomically with the HEAD CAS;
  - every coexisting canonical HEAD writer and frontier LWT participates in
    the same compatible global `SERIAL` Paxos domain;
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
- a test-only GC interleaving model proving that late library liveness does not
  revoke already-won destructive authority and that the required
  `resolve/capture exact physical incarnation P → durable liveness → exact-P +
  GC-authority`
  order is enforced;
- a test-only inductive frontier model proving only `(H,H,V) → (H',H',V)` and
  rejecting a missing/stale predecessor witness, a wrong contract version, or
  a mismatched predecessor HEAD; mutations must make each predicate and both
  witness updates fail closed;
- a source-contract test that pins the single owner, the conditional meaning of
  `newly-live`, the exact witness rule, the non-expiring liveness requirement,
  the legacy deterministic-locator policy, the global `SERIAL` prerequisite,
  and the open issues;
- a Docker 3-DC probe using an ephemeral test-only table (no migration) that
  proves a stale witness CAS cannot certify `H'` after HEAD moved from `H`;
- a mutation script proving that removing the HEAD condition, claiming
  `newly-live` is unconditionally complete, changing the owner, or reordering
  the GC-aware baseline handshake makes tests RED;
- `go test ./... -short`, the PC-0/PC-1 contracts, the mutation suite,
  `go vet ./...`, and `git diff --check`, all run through Docker.

The issue is marked **decision resolved / implementation prerequisite open**.
W2, R31, X1, content resurrection, G4/G5, and
`ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` remain OPEN. No funnel is migrated,
no publication runtime changes, and no GC activation is permitted.
