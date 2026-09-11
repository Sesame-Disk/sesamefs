# PC-0 — Multi-DC Publication Protocol Characterization

**Status:** characterization only. No `PublicationCoordinator` is implemented.
**Baseline:** rebased onto `main` at `7b9102af9` (contains #209/#210/#212/#213).
Originally characterized against `a0eef9fb3` (contains #206/#208; did **not**
contain #209/#210); this file was re-characterized after #210, #212, and #213 landed
in `main`. #210's real 3-DC `EACH_QUORUM` fallback for Sync PutBlock cross-DC
provenance is recorded in §7, §8, §11, and §13. #213's shared repair
reachability classifier is recorded in §7–§9, §13, and §15: one `SERIAL`
HEAD read plus at most 1024 sequential `EACH_QUORUM` parent reads under a
30-second context; inconclusive evidence remains `UNKNOWN` and retains repair.
#209 (G2 PREPARED→COMMITTED handoff) touches only `internal/gc` and is
orthogonal to the publication funnels characterized here. The merged #212 (G3 canonical retirement) change is also GC-side and orthogonal to the publication funnels characterized here; no PC-0 funnel re-characterization is required.
**Scope:** documentation, source-contract tests, test-only 3-DC characterization harness.
**Not closed:** W2, R31, X1, G4/G5. G3 canonical retirement is implemented by #212. `GC_ENABLED=false` remains required.
**Verdict:** `PROCEED WITH COORDINATOR` — see §14.
**Audit pass 2026-09-10 (ninth — deep audit with report):** the inventory was incomplete in three ways
that this pass closes as characterization only. (1) `libraries.head_commit_id`
has **six** writers, not two: two CAS primitives, two creation-time INSERTs,
and two **unconditional UPDATE initializers** that call no HEAD helper and
were invisible to the lexical guard; the unconditional shape was reproduced
reverting an LWT-published HEAD from a blind DC on the real 3-DC fixture
(§3.4, §7, §15). (2) `RevertFile` / `RevertDirectory` / `RestoreTrashItem` /
`RevertDirents` were misclassified as tree-only; they publish a positive
borrowed block-dependency delta with no `pub:`, repair, or fence (§3.5).
(3) GC Phase 5's expired-version cascade deletes fs_objects shared with the
live HEAD, so "ordinary GC reachability" is **not** an available answer to
`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01` (§6, §10, §15). §11 (PutCommit
timing) and §12 (per-publish tree-stats walk) were corrected. None of these
is fixed here; all are pre-existing.

This file answers:

> What distributed protocol do today's writers already execute when they turn
> prepared blocks into a safe HEAD publication, and what is the correct boundary
> of a future multi-DC coordinator?

It does **not** introduce the coordinator, migrate funnels, change Cassandra
schema, change production consistency, or close W2/R31.

Related live documents:

- [R3-LIVENESS-CONTINUITY.md](./R3-LIVENESS-CONTINUITY.md) — per-provenance
  liveness inventory. This file does not replace it.
- [OPEN-WORK-INDEX.md](./OPEN-WORK-INDEX.md) — live status.
- [KNOWN_ISSUES.md](./KNOWN_ISSUES.md) — findings of record.
- [GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md](./GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md) — R31/X1.

---

## 1. Scope

### In scope

Characterize every productive HEAD publisher that can stage new block
liveness (`pub:` / publish-attempt refs) and CAS library HEAD; inventory
**every** writer of `libraries.head_commit_id`, including raw-CQL writers that
call no HEAD helper; reconstruct the
real `up → pub → HEAD → fs` protocol; classify consistency/visibility;
record crash/ambiguity/cleanup semantics; separate universal steps from
funnel-specific preparation; investigate whether Sync already has a durable
PutBlock→HEAD identity; pin selected inventory/consistency tokens with source
contracts.

### Out of scope

Implementing `PublicationCoordinator`; migrating funnels; production behavior
changes; schema/CL/TTL/GC/G2–G5; closing W2/R31/X1; Seafile client protocol
changes; new receipts/tokens; absorbing discovered bugs into this PR.

Findings are recorded, not fixed.

### Runtime change

**Observed production diff for this PR: documentation + tests/test hooks only.**
No CQL, schema, config, or productive branch.

---

## 2. Terminology

These names are **control-flow phases**, not Cassandra tables, unless a later
row says a phase is durable.

| Name | Meaning |
|---|---|
| **Funnel** | An endpoint/path that can publish new block liveness through HEAD. |
| **Adapter / evidence provider** | Funnel-specific preparation: bytes, canonical IDs, provenance, exact P, and the work that turns a classified block into a *publishable* input. Classification is not authorization. |
| **Own liveness** | A writer-owned `up:` referrer this process (or an equivalent retry) created. |
| **Exact P** | The currently observed canonical physical placement `(storage_class, storage_key)`. |
| **Expected P** | The placement an adapter captured and carries forward on `PublishableInput` (e.g. F3's `commitBlockPlacement`). In the current F3 path it is captured during verification for every ready `SessionUpload` or `BorrowedFS` block, before `ensureCommitBlockOwnLiveness`; it is a value to be checked later, not proof placement still holds, and capturing it is **not** the final exact-P revalidation. |
| **Publication authority / continuity** | Every physical dependency that a HEAD will newly live on must arrive at that HEAD with continuous valid liveness for its provenance. Depending on provenance, that may be own pin + exact-P, continuous renewal/overlap, or both. Exact-P revalidation is one mechanism, not the universal recipe. |
| **Classified input** | A block sorted as `OWNED` / `BORROWED` / `UNPROVENANCED` / `ERROR`. Classification does not make it publishable. |
| **Publishable input** | Target coordinator input: classified input that has durable writer-owned liveness for every physical dependency HEAD will newly live on. `OWNED` keeps/renews its `up:`. `BORROWED` must **acquire** durable own `up:` first (W1); exact-P revalidation of the foreign `fs:` does **not** substitute for that pin. `UNPROVENANCED` and `ERROR` are rejected and **do not** produce `PublishableInput`. When the adapter carries `ExpectedP`, that value is captured evidence, **not** the final exact-P revalidation; the final revalidation happens after stage and before HEAD, while its order relative to repair is funnel-specific (§4, §10, §14). Today's funnels do not all satisfy this target contract. |
| **`pub:` / publish attempt** | Attempt-local provisional referrer keyed by the publication attempt/commit. |
| **Durable repair** | `published_block_reference_repairs` row that can outlive the request. |
| **HEAD CAS** | Conditional `UPDATE libraries ... IF head_commit_id = ?`. |
| **APPLIED** | The CAS is known to have made `commitID` the canonical HEAD. |
| **KNOWN_LOSER** | CAS returned `applied=false` with a definite current HEAD. |
| **UNKNOWN** | The CAS may have applied; confirmation failed or was never attempted. |
| **Settlement** | Promote `pub:` → `fs:`, retain repair, or exact cleanup of a known-loser attempt. |
| **Coordinator** | Logical owner of the common protocol, runnable identically on every node. Not a leader, mutex, or home-DC process. |

Do not reuse GC names `PREPARED` / `COMMITTED` for these phases.

`OBSERVED` means the current code does this. `REQUIRED` means a future
coordinator (or W2) would need this property. Freezing an OBSERVED CL does
not make it REQUIRED.

**Scope caveat (`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`):** "newly
live on" above is deliberately the same shape as R3's
`LogicalPositiveBlockDelta` (new HEAD's reachable blocks minus old HEAD's).
R3 itself states that delta is **not** the complete work set: dependencies
inherited unchanged from the old HEAD, whose continuity was never proven or
was only `CONDITIONAL`/`UNKNOWN` when they first entered some earlier HEAD,
are not covered by "newly live on" and are not covered by `PublishableInput`
below. The current GC is **not** a defense for those inherited dependencies:
Phase 5's expired-version cascade deletes fs_objects still reachable from
HEAD (§6 PUBL-10, counterexample frozen in `internal/gc`). The open decision,
to be made with evidence before PC-2, is therefore whether a *repaired,
sharing-aware* GC can assume that responsibility, or whether the
coordinator's work set must include inherited dependencies whose continuity
was never proven. See §6 (PUBL-1/PUBL-2/PUBL-10), §10, and §15.

---

## 3. Inventory of HEAD publishers

Guards: `TestPC0AllHeadCallersAreInventoried` (lexical named HEAD calls in
the inventoried functions, not every callsite shape),
`TestPC0RawHeadColumnWritersAreInventoried` (every production string literal
under `internal/` and `cmd/` that writes `libraries.head_commit_id`, with its
write shape — the raw-CQL blind spot of the lexical guard),
`TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams` (§3.5),
`TestPC0BlockPublicationFunnelsHaveMappedSeams`,
`TestPC0R3StageToHeadInventoryIsSubset` (compares against the live R3
`r3PublicationStageToHeadBoundaries` list, not a duplicate copy),
`TestR3PublicationStageToHeadHasNoUnlistedDirectDBCalls` (R3 baseline).

The lexical parser retains receiver identity for methods, so same-named
methods in one source file cannot silently overwrite one another.

### 3.1 Block-publication funnels (enter `pub:` then HEAD)

| ID | Endpoint / wrapper | Prepare | Provenance | Stage | Readiness / exact P | Repair | HEAD helper | Settlement |
|---|---|---|---|---|---|---|---|---|
| F1 | `v2.CreateFile` | Office template PUT **or** empty file | Template: `RegisterUploadedBlockTargetAndMapping` with fresh `up:<uuid>`; empty file: no blocks | `stagePendingPublishedFiles` | **none** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule repair; known-loser request-local cleanup |
| F2 | `v2.UploadFile` → `finalizeStoredUploadMetadata` → `Once` | stored upload / SeafHTTP-compatible v2 | `RegisterUploadedBlockTarget` during upload (`up:<operation>`) | `stagePendingPublishedFiles` | `validateCommitBlockPublicationFences(commitBlocks)` — **no-op: UploadFile passes `nil`** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule; known-loser cleanup |
| F3 | `v2.CreateFileFromBlocks` → same `Once` | session blocks + BorrowedFS | `ensureCommitBlockOwnLiveness` (`up:<session>`) before claim | same `Once` | **yes**: `validateCommitBlockPublicationFences(commitBlocks)` after stage and repair, before HEAD | same | same | same; session claim is funnel-specific |
| F4 | `v2.processSingleItem` cross-repo copy/move | `copyFSObjectToLibraryForPublish` | source-repo `fs:` borrowed; **no destination own pin proven** | `stagePendingPublishedFiles` when `pendingCopiedFiles > 0` | **none** | `queuePendingPublishedFileRepairs` | dest `UpdateLibraryHeadFromSnapshot` | promote dest refs; source HEAD (move) is a separate tree mutation |
| F5 | `v2.publishEditedDocumentMetadata` (OnlyOffice) | callback download + `saveOnlyOfficePendingBlock` | callback `up:<operation>` | `stagePendingPublishedFiles` | **none** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule; pending-commit-id is OO-specific |
| F6 | `SeafHTTP.commitUploadedFileOnce` | single-block upload in this request | register during upload | `stageSeafHTTPPublishAttemptReferences` | **none** | `queuePublishedFSObjectBlockReferenceRepairFn` | `UpdateLibraryHeadFromSnapshot` | `finalizeSeafHTTPPublishedBlockReferences` |
| F7 | `SeafHTTP.commitUploadedFileMultiBlockOnce` | multi-block upload | register during upload | same | **none** | same | same | same |
| F8 | `Sync.handleSyncHeadPromotion` | client PutBlock / RecvFS / PutCommit | Scope gate: LQ hit ⇒ provenanced; clean LQ miss ⇒ EQ hit provenanced / EQ miss unprovenanced / EQ error abort; unprovenanced blocks skip readiness | `stageSyncCommitBlockDelta` **before** readiness | `ensureSyncCommitBlockPublicationReadiness` (provenanced subset only) | `queueSyncCommitBlockReferenceRepairsFn` **after** readiness | `updateLibraryHeadWithStats` | `finalizeSyncCommitBlockDeltaAndSettleRepairIntent`; shared repair never cleared on request-local loss |
| F9 | `Sync.tryAutoMergeSyncHeadPromotion` | merge commit of target onto current HEAD | same complete LQ→EQ scope gate and provenanced subset | `stageSyncCommitBlockDelta` | same | queue after readiness; auto-merge commit ID includes a fresh UUID | `updateLibraryHeadWithStats` | structurally unique attempt: cleanup of `pub:` is safe; settlement still required |

`CreateFileFromBlocks` is **not** a distinct HEAD callsite. It is an adapter
in front of F2's finalizer, and the only current caller that populates
`commitBlocks` so F2's fence actually runs.

### 3.2 HEAD mutations that are not block publication

These CAS HEAD but do not stage new `pub:` block liveness. They are inventoried
so a future publisher cannot hide as "just another tree mutation":

`CreateDirectory`, `RenameDirectory`, `RenameFile`, `DeleteDirectory`,
`DeleteFile`, `BatchDeleteItems`, `copyItemWithinRepoWithRetry` (same-repo
copy), `processSameRepoMove`, and the **source-library** HEAD in a
cross-repo move (`processSingleItem`).

R3 already excludes same-repo copy/move from the publication inventory. PC-0
agrees.

`RevertFile`, `RevertDirectory`, `RestoreTrashItem`, and `RevertDirents` were
listed here until the 2026-09-10 audit. They are **not** tree-only: they make
the new HEAD depend on historical fs_objects the old HEAD did not depend on.
They are reclassified as content-resurrection publication paths in §3.5.

`TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams` is the negative
classification guard: an entry currently classified as a tree mutation must
not call the known block-publication stage seams
(`stagePendingPublishedFiles`, `stageSeafHTTPPublishAttemptReferences`, or
`stageSyncCommitBlockDelta`). If a future tree path does call one, the entry
must be reclassified and remapped as a publication funnel rather than silently
inheriting the tree-only classification.

### 3.3 Indirect wrappers

| Wrapper | Delegates to |
|---|---|
| `CreateFileFromBlocks` | `finalizeStoredUploadMetadata` |
| `finalizeStoredUploadMetadata` | `finalizeStoredUploadMetadataOnce` (retry on `ErrLibraryHeadConflict`) |
| `UploadFile` | `finalizeStoredUploadMetadata(..., nil)` |
| `commitUploadedFile` / `commitUploadedFileMultiBlock` | corresponding `Once` |

The two CAS primitives are `FSHelper.UpdateLibraryHead` (LWT;
`UpdateLibraryHeadFromSnapshot` is the v2/SeafHTTP/OnlyOffice entry) and
Sync's `updateLibraryHeadWithStats`. They are **not** the only writers of
`libraries.head_commit_id` — see §3.4.

### 3.4 HEAD initializers (resolved 2026-09-11: conditional, inside the CAS domain)

**Status:** the unconditional shape described below was **retired on
2026-09-11** (branch `fix/h1-conditional-head-initializer`,
`ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01` resolved). Both initializers now
publish through `FSHelper.InitializeLibraryHeadIfUnset`
(`IF head_commit_id = null AND created_at != null`), so
`TestPC0RawHeadColumnWritersAreInventoried` now lists **five** writers (two
CAS advances, one CAS initializer, two creation-time INSERTs) and
`TestPC0NoUnconditionalHeadUpdateRemains` fails on any reintroduction.
Handler-level 3-DC evidence: `scripts/h1-initial-head-multidc-validation.sh`.
The characterization of the pre-fix state is kept below as the record of
what the audit found and why it was a coordinator prerequisite.

At audit time (2026-09-10) `TestPC0RawHeadColumnWritersAreInventoried`
scanned every production string literal that writes
`libraries.head_commit_id`. There were six, in three shapes:

| Writer | Shape | Trigger | Row exists before? |
|---|---|---|---|
| `FSHelper.UpdateLibraryHead` | CAS `IF head_commit_id = ?` | every v2/SeafHTTP/OO publish | yes |
| `SyncHandler.updateLibraryHeadWithStats` | CAS `IF head_commit_id = ?` | Sync direct HEAD / auto-merge | yes |
| `CreateLibrary` (`libraries.go`) | `INSERT` at creation, fresh UUID partition | user library creation | no |
| `AdminCreateLibrary` (`admin_libraries.go`) | `INSERT` at creation, fresh UUID partition | admin library creation | no |
| `FSHelper.InitializeLibraryFS` | **was:** `UPDATE … SET head_commit_id = ?` in a `LoggedBatch`, no `IF`; **now:** `InitializeLibraryHeadIfUnset` (CAS) | group / org-admin / admin-extra library creation, a separate batch **after** the `libraries` INSERT | yes |
| `SyncHandler.createInitialCommit` | **was:** `UPDATE … SET head_commit_id = ?` in a `LoggedBatch`, no `IF`; **now:** `InitializeLibraryHeadIfUnset` (CAS) | **`GET /seafhttp/repo/:id/commit/HEAD`** whenever a session-consistency read returns `""` | yes |

The two creation-time INSERTs write a partition nobody else can address yet;
they are inventoried, not flagged. The two `UPDATE` initializers were the
finding: they mutated an **existing** row after a `LOCAL_QUORUM` read, with no
LWT, and a `GET` could trigger one of them. `TECHNICAL-DEBT.md` §19.e recorded
the premise "the library has no concurrent writers at first-touch"; that
premise is false in multi-DC:

```text
dc-na:  library row exists, head=''      (INSERT replicated everywhere)
dc-na:  HEAD := C1  via CAS IF head_commit_id=''   (SERIAL, applied)
dc-eu:  replica still holds head=''      (lag, partition, hints pending)
dc-eu:  GET /commit/HEAD → LOCAL_QUORUM read '' → createInitialCommit
        → LoggedBatch UPDATE head=C0 (no IF)
result: every DC converges to C0 by timestamp; C1 (with files) is no longer HEAD
```

Reproduced 2026-09-10 on the real 3-DC fixture with the exact CQL shapes
(`scripts/pc0-initial-head-xdc-probe.sh`; hints off, dc-eu stopped during the
CAS, restarted blind): `dc-na SERIAL head='C0-initial-empty'` in every DC. The
control leg — the same initialization expressed as `IF head_commit_id = ''`
from the blind DC — is rejected and reports the real HEAD. This is
`ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01` (its multi-DC reversion variant is
newly recorded); it was pre-existing, not introduced by PC-0, and was fixed
in the separate follow-up as a coordinator prerequisite: a coordinator that
classifies `APPLIED` on the CAS domain cannot be correct while a non-CAS
writer can move HEAD backwards. The fix is the conditional initializer
(`IF head_commit_id = null AND created_at != null`; the `created_at` guard is
load-bearing because `IF head_commit_id = null` alone upserts a phantom row
on a missing partition). The outcome is tri-state (APPLIED /
ALREADY_INITIALIZED = demonstrated KNOWN_LOSER / UNKNOWN); only a KNOWN_LOSER
may discard its attempt-unique commit row, best effort. `GET /commit/HEAD`
still initializes an uninitialized library, but only through that path, and
returns the HEAD the Paxos round settled on once that HEAD's commit is
servable locally (else `503 Retry-After`) — the blind datacenter answers
with the real, usable HEAD. The guard freezes the five remaining writers and
their shapes and pins both CAS clauses;
`scripts/pc0-initial-head-xdc-probe.sh --expect-cas-fix` validates the CQL
shape and `scripts/h1-initial-head-multidc-validation.sh` validates the
production code on the real 3-DC fixture.

### 3.5 Content-resurrection publication paths (R1–R4)

| ID | Endpoint | What it publishes | Provenance | `pub:` | Repair | Fence |
|---|---|---|---|---|---|---|
| R1 | `v2.RevertFile` | file fs_object taken from a historical commit | BORROWED (historical `fs:`) | none | none | none |
| R2 | `v2.RevertDirectory` | directory subtree from a historical commit | BORROWED | none | none | none |
| R3 | `v2.RestoreTrashItem` | deleted entry taken from the commit that still had it | BORROWED (trash) | none | none | none |
| R4 | `v2.RevertDirents` | batch of R3 | BORROWED (trash) | none | none | none |

Each reads an old commit, lifts an `oldEntry`/fs_object out of it, inserts
it into the current tree, creates a new commit, and CASes HEAD. No bytes move,
but the new HEAD **newly lives on** every block under that fs_object — a
positive `LogicalPositiveBlockDelta` by PC-0's own definition (§2). The only
liveness those blocks have is the historical `fs:<library>:<fs_id>` reference,
which GC retention (trash / version TTL) is entitled to remove concurrently.
Today these paths are weaker than cross-repo (F4), which at least stages
`pub:` and queues repair. They are inventoried as
`pc0HeadContentResurrection`; `TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams`
freezes the observed absence of seams and forces reclassification as a
block-publication funnel when one of them is migrated. Recorded as
`ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` (§15); the target coordinator
must treat them as `BORROWED` adapters that acquire a durable own pin on the
resurrected fs_object's blocks (and `ExpectedP`) before staging. Not fixed
here.

---

## 4. Reconstructed current protocol

Observed productive control flow, not a new state machine table. There is no
universal `BLOCKS_CLASSIFIED → PUBLISHABLE → PUB_STAGED` sequence in today's
code: CFFB captures placement and acquires own liveness before staging, while
Sync stages before its provenance/readiness checks. The stable common kernel
starts only once a funnel has chosen to stage:

```text
FUNNEL-SPECIFIC PREPARE / CLASSIFY
        ↓
PUB_STAGED               // attempt-local pub: — durable row, TTL-bound
        ↓
        ┌───────────────────────────────┐
        │ durable repair intent          │
        │ (for every block dependency)   │  required before HEAD;
        │ funnel-specific readiness      │  readiness is optional and
        │ / final exact-P fence           │  also before HEAD when present.
        └───────────────────────────────┘
        ↓
HEAD_ATTEMPTED          // LWT on libraries.head_commit_id
        ├── APPLIED
        ├── KNOWN_LOSER
        └── UNKNOWN
                ↓
          SETTLEMENT
                ↓
       FS_DURABLE / CLEANED / RETAINED
```

For every current block-bearing funnel **F1–F9**, the durable repair intent
precedes HEAD: `stage pub < durable repair < HEAD`. An empty-file path with
zero physical dependencies may legitimately produce no repair row; that
degenerate case does not make repair optional when dependencies exist. The
kernel below does **not** describe the content-resurrection paths R1–R4
(§3.5: positive delta, no stage, no repair, no fence) nor the HEAD
initializers (§3.4: no CAS at all); both are recorded gaps, not exceptions
the kernel tolerates. Readiness, when a funnel has
it, also precedes HEAD, but its order relative to repair remains funnel-specific.
These facts are frozen by the funnel seam contract and the order tests below;
the coordinator diagram in §14 is a target boundary, not a description of every
current funnel.

### Which phases are durable vs control-flow

| Phase | Durable today? | Notes |
|---|---|---|
| Funnel prepare (bytes in object store, session, OO download) | funnel-specific | Not a common table. |
| Own `up:` | yes, `block_references` + TTL | Identity is funnel-specific. |
| Exact P observation | attempt-local memory (`commitBlockPlacement` / `syncCommitBlockPlacement`) | Not persisted as a publication intent. |
| `pub:` stage | yes, TTL-bound provisional refs | Attempt-local referrer. |
| Repair row | yes, ordinary write | Shared by Sync direct-HEAD `commit_id`; unique per v2/SeafHTTP/OO attempt commit. |
| HEAD | yes, `libraries` LWT | Paxos/serial domain. |
| Classification APPLIED/LOSER/UNKNOWN | **not** a persisted enum | Inferred from CAS result + optional SERIAL confirm (v2) or `errSyncHeadCASUncertain` (Sync). |
| `fs:` promotion | yes | Settlement. |
| Known-loser witness | **no** durable loser row | `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01`. |

A new Cassandra table is **not** implied by this state machine. Repair + `pub:`
+ HEAD already carry crash recovery for APPLIED/UNKNOWN. Known-loser is the
gap.

### Order is not identical across funnels

There is **no** universal `stage → readiness → repair → HEAD` sequence.
`TestPC0ObservedRepairReadinessPartialOrder` plus
`TestPC0BlockBearingFunnelsKeepDurableRepairBeforeHEAD` freeze the observed
orders below. Unifying them is a later PR with explicit evidence, not a PC-1
default.

```text
CreateFileFromBlocks / shared Once (when commitBlocks is populated):
  own up:<session>  →  claim session  →  stage pub  →  queue repair  →  exact-P  →  HEAD

Sync direct HEAD / auto-merge:
  stage pub  →  readiness (renew up + exact-P, provenanced only)  →  queue repair  →  HEAD

CreateFile / stored UploadFile / OnlyOffice / SeafHTTP / cross-repo:
  stage pub  →  queue repair  →  HEAD
  (no pre-HEAD readiness/exact-P; own liveness is whatever the prepare step left)
```

The common kernel is the following **partial order** for block-bearing
publications:

```text
                 ┌→ durable repair intent ──┐
stage pub ────────┤                           ├→ HEAD
                 └→ readiness/final exact-P ┘
                    (when present; also before HEAD; relative order is funnel-specific)
```

An empty-file path with no physical dependencies may skip the repair row. Do
not freeze readiness-before-repair as the coordinator spine: CFFB requires
repair-before-fence, while Sync requires readiness-before-repair. The other
current block-bearing funnels use stage → repair → HEAD. Any future unification
must preserve the observed orders unless a separate PR provides evidence.

---

## 5. Per-funnel matrix

W2 status uses R3's exact classification vocabulary
(`docs/R3-LIVENESS-CONTINUITY.md`): `PROVEN_CONTINUOUS` / `CONDITIONAL` /
`UNGUARDED` / `UNKNOWN`. Every row in the per-funnel matrix below currently
lands on `CONDITIONAL` or `UNKNOWN`; none is `PROVEN_CONTINUOUS`, and none
is `UNGUARDED` (no funnel has a proven concrete zero-reference interleaving).
PC-0 does not shorten this to a three-value scheme and does not rename
`PROVEN_CONTINUOUS` to `PROVEN`.

### F1 — v2 CreateFile

| Dimension | Observed |
|---|---|
| Content origin | Server-side Office template bytes, or empty file |
| Operation identity | Fresh UUID `uploadOperationID` per HEAD retry |
| Block identity | SHA-256 at template hash time; SHA-1 stored as external id |
| Provenance | `RegisterUploadedBlockTargetAndMapping` after PUT/reuse |
| Own liveness | `up:<uuid>`; empty files have none |
| TTL | 48h provisional |
| Dedup | `ProbeBlockReuse` on template |
| Exact P | Known at materialize; **not re-validated before HEAD** |
| Fence | none |
| `pub:` | `stagePendingPublishedFiles` / `AddPublishAttemptReferences` |
| Durable repair | yes, before insertCommit/HEAD |
| HEAD | `UpdateLibraryHeadFromSnapshot` |
| Success | CAS `applied=true`; derived-state failure is logged, not rolled back |
| Known loser | `ErrLibraryHeadConflict` → `CleanupFailedPublishAttempt` + clear repair |
| Unknown | `ErrLibraryHeadPublicationUnknown` does **not** take the conflict cleanup branch |
| Cleanup | request-local on known loser; UNKNOWN retains |
| Crash | repair worker; no loser witness |
| Multi-process | repair row is Cassandra; another pod can settle APPLIED |
| Multi-DC | HEAD is global LWT (shipped `SERIAL`); `pub:`/repair ordinary writes |
| CL | session `LOCAL_QUORUM`; HEAD LWT uses session `serial_consistency` |
| Paxos | HEAD CAS only (plus any materialize install LWT, funnel-specific) |
| Cost | O(1) file; O(blocks) stage (0 or 1); no per-block pre-HEAD fence |
| W2 | `CONDITIONAL` (template) / n/a empty |
| Common | stage, repair, HEAD, classify, settle |
| Specific | template materialize, empty-file, UUID operation, retry wrapper |

### F2 — stored v2 upload (`UploadFile` / `finalizeStoredUploadMetadataOnce`)

Same finalizer as F3, but `commitBlocks == nil`, so
`validateCommitBlockPublicationFences` returns immediately. Exact-P is
**implemented in the shared function and unused by this caller**.

| Dimension | Observed |
|---|---|
| Content origin | Client bytes via v2 upload-link / `UploadFile` |
| Operation identity | Upload operation id from materialize |
| Block identity | SHA-256 at PUT; fs_object may hold SHA-1 |
| Provenance | `RegisterUploadedBlockTarget` `up:<operation>` |
| Own liveness | 48h `up:`; not renewed at finalize |
| Exact P / fence | **absent at finalize** |
| Repair / HEAD / settle | same as F3's finalizer |
| W2 | `CONDITIONAL` (R3); publication-authority/continuity at finalize is **UNKNOWN/absent** |
| Common | stage, repair, HEAD, settle |
| Specific | upload materialize; nil placements |

Finding: `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` (publication-readiness/authority gap
by provenance; not a prescription that every funnel must run exact-P).

### F3 — CreateFileFromBlocks

| Dimension | Observed |
|---|---|
| Content origin | Previously uploaded session blocks and/or foreign `fs:` |
| Operation identity | `session_id` (durable) + derived commit id |
| Block identity | Canonical SHA-256 before claim; SHA-1 for fs_object |
| Provenance | `SessionUpload` vs `BorrowedFS` vs `None` |
| Own liveness | `ensureCommitBlockOwnLiveness` upgrades/renews `up:<session>` after placement capture |
| Exact P | `commitBlockPlacement` captured at verify for every ready `SessionUpload` and `BorrowedFS` block; fenced immediately before HEAD |
| Fence | `ValidateBorrowedFSPublicationAuthority` @ `BlockAuthorityAdvisory` (LQ) |
| Repair | after stage, before fence/HEAD |
| Crash | session claim + repair row; session result recorded after success |
| Multi-process | session LWT claim is the adapter lock; repair is shared Cassandra |
| Multi-DC | pin is durable; session claim is a Cassandra serial-domain LWT; fence is LQ (safe because own pin is already written) |
| Cost | Pre-HEAD: O(distinct blocks) readiness + one session-claim LWT + one HEAD LWT; a successful request may add one slot-release LWT; O(files) repair |
| W2 | `CONDITIONAL` through pre-HEAD (W1/W2 slices); R31 open |
| Common | proven blocks, stage, repair, exact-P, HEAD, settle |
| Specific | session claim, `/blocks/check`, BorrowedFS classify, digest idempotency |

This is the **clearest** current embodiment of the candidate coordinator
kernel, but it is not a universal current funnel. `commitBlockPlacement` is
the `ExpectedP` carrier for both `SessionUpload` and `BorrowedFS`; the observed
order is verify/capture placement → acquire or renew own liveness → stage →
repair → final exact-P fence → HEAD. Its repair-then-fence order is not frozen
as the common spine.

### F4 — cross-repo copy/move

| Dimension | Observed |
|---|---|
| Content origin | Source library fs_objects/blocks, not a new PUT |
| Operation identity | dest commit id |
| Provenance | source `fs:`; destination does not take `up:<dest-session>` |
| Own liveness | **not proven** (R3 `UNKNOWN`) |
| Exact P | none |
| `pub:` | dest attempt refs for copied files |
| Repair | yes when copied files exist |
| HEAD | dest first; move then mutates source HEAD (tree-only) |
| Multi-DC | dest publication is a normal HEAD; borrowed source liveness is not fenced |
| W2 | `UNKNOWN` |
| Common | stage/repair/HEAD/settle **if** copied files exist |
| Specific | copy fs_objects across libraries, conflict policy, source deletion |

Cross-repo is the weakest adapter. It still uses the same stage→repair→HEAD
spine once files are copied.

### F5 — OnlyOffice

| Dimension | Observed |
|---|---|
| Content origin | OO callback download |
| Operation identity | `pendingOperationID` + commit id |
| Provenance | callback `up:` |
| Exact P | none at publish |
| Extra durable step | `updateOnlyOfficePendingBlockCommitID` before HEAD |
| W2 | `CONDITIONAL` (R3) |
| Common | stage, repair, HEAD, settle |
| Specific | OO download, pending-block commit id, storage-delta vs original size |

### F6/F7 — SeafHTTP single and multiblock

| Dimension | Observed |
|---|---|
| Content origin | Seafile HTTP upload |
| Operation identity | derived commit id (`sha1(repo:root:desc:unixnano)`) |
| Provenance | register during upload, not renewed at commit |
| Exact P | none at commit |
| Stage | `stageSeafHTTPPublishAttemptReferences` |
| Extra | direct `INSERT INTO commits` in the stage→HEAD window (frozen R3 CQL=1) |
| Repair | yes |
| W2 | `CONDITIONAL` |
| Common | stage, repair, HEAD, settle |
| Specific | Seafile fs_object layout, context cancel checks, commit INSERT |

### F8/F9 — Sync direct HEAD and auto-merge

| Dimension | Observed |
|---|---|
| Content origin | Desktop client PutBlock / RecvFS / existing blocks |
| Operation identity | **target commit id** (client PutCommit); auto-merge adds UUID attempt |
| Block identity | positional canonical map at stage |
| Provenance | `BlockReferenceExistsLocalQuorum(up:sync:<repo>:<block>)`; a clean local miss escalates to `BlockReferenceExistsEachQuorum` before absence is accepted |
| Own liveness | renewed/acquired only for the provenanced subset; a clean global miss never fabricates liveness |
| TTL | 48h; expiry ⇒ treated as unprovenanced (`ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01`) |
| Dedup | CheckBlocks can skip PutBlock entirely ⇒ no `up:` |
| Exact P | only provenanced subset; `ProbeBlockReuse` + advisory fence |
| `pub:` | staged **before** readiness |
| Durable repair | after readiness; shared by `commit_id` for direct HEAD |
| HEAD | `updateLibraryHeadWithStats` (not `UpdateLibraryHead`) |
| Success | `applied=true` then counters + derived state |
| Known loser | `syncHeadConflictError`; same-target treated as idempotent success; divergent retains shared repair, cleans this attempt `pub:` |
| Unknown | **any** CAS error → `errSyncHeadCASUncertain`; **no SERIAL confirm** |
| Cleanup | attempt `pub:` only; shared repair retained until positive settlement |
| Crash | repair + `pub:`; crash after known loser before cleanup ⇒ UNKNOWN retain |
| Multi-process | yes for settlement; PutBlock identity is the deterministic `up:` key |
| Multi-DC | LQ miss escalates to an `EACH_QUORUM` fallback before being treated as absence (#210, resolved 2026-09-08); a **global** miss still ⇒ skip readiness for that block (unprovenanced path, not a rejection) |
| CL | scope gate: LQ for every candidate, EQ only after a clean LQ miss; remaining readiness for provenanced blocks; fence LQ; HEAD LWT; repair cold path SERIAL + parent EACH_QUORUM |
| Paxos | HEAD CAS; not per-block |
| Cost | scope gate: O(all distinct candidate blocks) LQ plus one EQ for each clean local miss; remaining readiness is O(provenanced blocks); O(added files) repair; O(1) HEAD |
| W2 | `CONDITIONAL` for PutBlock-visible subset; `UNKNOWN` without PutBlock |
| Common | stage, readiness, repair, HEAD, classify, settle |
| Specific | commit parent/ancestry, auto-merge, RecvFS, CheckBlocks, stats/counters |

### I1/I2 — HEAD initializers (`InitializeLibraryFS`, `createInitialCommit`)

| Dimension | Observed |
|---|---|
| Content origin | empty root fs_object + initial commit row |
| Operation identity | commit id derived from `(repo, root, time)`; same-second Sync requests derive the same id |
| Provenance / own liveness | none needed (no blocks) |
| HEAD | **unconditional** `LoggedBatch` `UPDATE libraries SET head_commit_id` at session `LOCAL_QUORUM`; no `IF` |
| Classification | none — there is no APPLIED/LOSER/UNKNOWN; a stale read simply wins by timestamp |
| Multi-DC | a blind DC's `""` read authorizes an overwrite of a HEAD another DC published by CAS (reproduced, §3.4) |
| W2 | n/a for blocks; **HEAD monotonicity is not guaranteed** |
| Disposition | pre-existing `ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01`; conditional initializer is a separate follow-up and a coordinator prerequisite |

### R1–R4 — content resurrection (`RevertFile`, `RevertDirectory`, `RestoreTrashItem`, `RevertDirents`)

| Dimension | Observed |
|---|---|
| Content origin | historical fs_object from an old commit / trash |
| Block identity | whatever the historical fs_object records |
| Provenance | BORROWED (historical `fs:` reference only) |
| Own liveness | **none** |
| Exact P / fence | none |
| `pub:` / repair | none |
| HEAD | `UpdateLibraryHeadFromSnapshot` (CAS) |
| Multi-DC | the borrowed `fs:` may be removed by GC retention in any DC while HEAD is being published |
| W2 | `UNKNOWN` (no proven continuity for the resurrected blocks) |
| Common | HEAD, classify |
| Specific | history/trash lookup, conflict policy, storage-quota delta |

Cost buckets for the scope-gate read, post-#210 (see §12 for the full table):

```text
local hit:                     LQ → remaining readiness → repair → HEAD
local error:                   LQ error → ABORT (no EQ, repair, or HEAD)
clean local miss + remote hit: LQ + EQ → remaining readiness → repair → HEAD
clean local miss + EQ error:   LQ + EQ error → ABORT (no repair or HEAD)
clean local miss + global miss: LQ + EQ miss → unprovenanced → repair → HEAD
```

Only a clean local miss starts the EQ fallback. A local error fails closed
without EQ; an EQ error fails closed after the LQ+EQ attempt. In the observed
Sync order, `pub:` may already be staged before these readiness outcomes, but
error paths do not queue repair or attempt HEAD.

---

## 6. Candidate invariants (PUBL-1 … PUBL-10)

| ID | Universal in today's code? | Notes |
|---|---|---|
| PUBL-1 Proven/publishable input | **No** | Classification exists in some adapters; Sync unprovenanced blocks and cross-repo borrowed `fs:` still enter `stage pub:`; content-resurrection paths (§3.5) publish borrowed historical `fs:` without any pin, stage, or repair. `UNPROVENANCED` and `ERROR` are not publishable. `BORROWED` is not publishable until the adapter acquires durable own liveness; observing/revalidating foreign `fs:` is the W1 TOCTOU. The coordinator may accept only `PublishableInput`. Classifying those states inside the coordinator and then staging them would centralize the W2 hole (Sync without PutBlock still has no liveness attributable to the commit). `PublishableInput` as defined is scoped to dependencies newly live on the HEAD being published, not to dependencies inherited unchanged from the old HEAD (`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`). |
| PUBL-2 Publication authority / continuity | **No** | Exact-P before HEAD exists only for F3 placements and Sync-provenanced blocks. F2's fence is a no-op. That is a provenance-specific authority/continuity gap, not proof that every funnel must add a second exact-P read. Own `up:` + GC fence + install/repair can close materialization continuity via renewal/overlap; BorrowedFS/late pin still needs exact-P because the pin may arrive after GC won; cross-repo shows exact-P alone is TOCTOU without a dest own pin; content-resurrection paths have no pin and no exact-P at all (§3.5). |
| PUBL-3 No liveness gap | **Unproven (R31)** | Ordering aims at overlap; 48h TTL and `pub:` TTL still exist. |
| PUBL-4 Durable ambiguity | **Mostly** | UNKNOWN does not take known-loser cleanup. Repair row is the durable witness. Finite `pub:` TTL remains R31 (`ISSUE-GC-PUB-REF-ZERO-REF-01`). |
| PUBL-5 Known loser ≠ unknown | **Yes in request-local paths; no durable loser** | Classified differently; crash before cleanup collapses to UNKNOWN retain. |
| PUBL-6 Multi-DC absence | **Fixed for the scope-gate decision (#210, resolved)** | A `LOCAL_QUORUM` miss no longer settles the answer: `syncBlockHasOwnLivenessProvenanceFn` escalates to `BlockReferenceExistsEachQuorum` first, real 3-DC evidence attached. Only a **global** miss is treated as "no currently observable provenance" — still fail-open into the unprovenanced path (a separate, already-tracked W2 gap, not what #210 closed). Repair reachability fail-closes (retain). Destructive GC uses EACH_QUORUM (X2 closed) — different domain. |
| PUBL-7 Fail closed | **Yes for unavailable/error observations; by design open on a clean global miss** | Readiness failures abort before HEAD. A local scope-gate **error** fails closed without EQ. After a clean local miss, an `EACH_QUORUM` **error** also fails closed; an unavailable observation must not become absence. A clean global **miss** (successful read, no row) is treated as "no currently observable provenance" and takes the unprovenanced path. The #210 trade-off is specifically the clean **local** miss → EQ fallback needed to distinguish remote visibility from absence; #210 does not close or guarantee the global-miss W2 gap. Error and miss are distinct outcomes; do not collapse them. |
| PUBL-8 Restart independence | **Partial** | APPLIED/UNKNOWN can be settled from another process via repair. Original process is not required. Known-loser cleanup is request-local. |
| PUBL-9 HEAD advances only through the CAS domain | **Yes (since 2026-09-11)** | At audit time two unconditional `UPDATE` initializers (§3.4) moved HEAD outside the LWT domain after a session-consistency read, one reachable from a `GET`; reproduced reverting a CAS-published HEAD from a blind DC. Resolved by `InitializeLibraryHeadIfUnset`; pinned by `TestPC0NoUnconditionalHeadUpdateRemains`. |
| PUBL-10 Inherited dependencies are protected by GC reachability | **No (counterexample)** | GC Phase 5 (`scanExpiredVersions`) cascades a dangling commit's tree through `processCommit → processFSObject` and deletes content-addressed fs_objects still reachable from HEAD, without a keep-set (Phase 6 has one). `TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD` freezes the observed behavior. Option 1 of `ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01` is therefore not available as-is; dormant only while `GC_ENABLED=false` (`ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01`, P0 PRE-GC). |

None of these refutes a coordinator. They show the coordinator must **own**
PUBL-1…10 rather than copy today's omissions. PUBL-9 was fixed outside the
coordinator (2026-09-11) so that its APPLIED classification can mean
something.

---

## 7. Multi-DC visibility matrix

Shipped production: `CASSANDRA_CONSISTENCY=LOCAL_QUORUM`,
`CASSANDRA_SERIAL_CONSISTENCY=SERIAL`, NTS `dc-na/dc-eu/dc-asia`.
`ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01`: if an operator sets `LOCAL_SERIAL`,
HEAD is no longer globally unique. OBSERVED default is SERIAL. REQUIRED for
a coordinator: do not silently accept `LOCAL_SERIAL` as global publication
authority.

| Operation | Local hit sufficient? | Local miss authoritative? | Global proof required? | Failure mode |
|---|---:|---:|---:|---|
| Own-liveness evidence (Sync scope gate) | yes for **presence** (fast path) | **no** | **yes, on a clean local miss** (`BlockReferenceExistsEachQuorum`, #210, resolved 2026-09-08) | local miss escalates to `EACH_QUORUM`; a **global** miss ⇒ block left untouched, not fabricated (still not proof PutBlock never happened, see TTL expiry); global error fails closed exactly like a local error |
| CFFB/session `up:` write/renew | n/a (plain quorum upsert; an existing row renews and an expired row can be recreated) | n/a | no | upsert error aborts before HEAD |
| Exact-P validation | yes (advisory LQ) **given own pin already durable** | no (`Changed`/`Blocked`/error abort) | no | reject before HEAD; does not fabricate Authorized |
| `pub:` stage | n/a (write) | n/a | durability = session LQ write | stage failure ⇒ cleanup attempt, no HEAD |
| Repair intent | n/a (ordinary write) | n/a | must survive remote recovery | queue failure: Sync retains possible applied insert; v2 cleans attempt |
| HEAD CAS | Paxos domain (`SERIAL` default) | n/a | classify | UNKNOWN retained |
| v2 ambiguous CAS confirm | SERIAL read of HEAD | n/a | yes for APPLIED vs not | confirm error ⇒ `ErrLibraryHeadPublicationUnknown` |
| Sync CAS error | n/a | n/a | **no confirm** | always UNKNOWN (`errSyncHeadCASUncertain`) |
| Settlement promote `fs:` | session LQ writes | n/a | success is local quorum of the write | failure ⇒ schedule repair, do not unpublish HEAD |
| Repair reachability | no | **no** | SERIAL HEAD + up to 1024 sequential EACH_QUORUM parent reads under a 30-second context | positive reachability promotes; missing/error/timeout/cycle/bound/unavailable ⇒ UNKNOWN and retain (`ISSUE-PUBLISH-REPAIR-REACHABILITY-01` closed for the shared classifier; broader R31 remains open) |
| Known-loser cleanup | request-local | n/a | must not run on UNKNOWN | crash ⇒ retain as UNKNOWN |
| HEAD initialization (`GET /commit/HEAD` → `createInitialCommit`; `InitializeLibraryFS`) | n/a | **no (since 2026-09-11)** — a session-CL `""` read only *proposes* initialization; the CAS decides | Paxos (HEAD serial domain) | blind DC's proposal is rejected and the real HEAD is returned (`scripts/h1-initial-head-multidc-validation.sh`); before the fix a `""` read authorized an unconditional overwrite (reproduced 2026-09-10, `scripts/pc0-initial-head-xdc-probe.sh` bug mode) |
| Content resurrection (R1–R4) | n/a (no liveness read at all) | n/a | none | borrowed historical `fs:` can be removed by GC retention in any DC; no pin, no fence |

**Never:** `LOCAL_QUORUM miss` ⇒ globally absent. At audit time exactly one
productive path violated this — HEAD initialization (§3.4), which treated a
local `""` read as "no HEAD exists" and overwrote; resolved 2026-09-11. Since #210, the Sync scope
gate no longer treats a local miss as absence on its own — it escalates to
`EACH_QUORUM` first, matching this rule's spirit. It still uses a **global**
miss to skip W2 readiness for that block, not to authorize GC; that remains a
different, narrower gap than X2 (which is closed) and is not what #210
closed.

---

## 8. Consistency map (OBSERVED vs REQUIRED)

`TestPC0CriticalConsistencyPrimitivesArePinned` pins **selected source tokens**
at named primitives. It does **not** freeze this whole table, and it does
**not** pin `libraries` HEAD `serial_consistency` (`SERIAL` vs
`LOCAL_SERIAL`; see `ISSUE-LIBRARY-HEAD-SERIAL-DOMAIN-01`). Productive
enforcement of global HEAD serialization stays in that issue's PR.

| Primitive | OBSERVED CL | REQUIRED for coordinator? |
|---|---|---|
| `BlockReferenceExistsLocalQuorum` | `LOCAL_QUORUM` | OBSERVED fast path; same-DC, zero added WAN. A hit or a read error both settle the answer without escalating. |
| `BlockReferenceExistsEachQuorum` | `EACH_QUORUM` | OBSERVED cross-DC fallback (#210, resolved), reached only on a clean `BlockReferenceExistsLocalQuorum` miss; REQUIRED to stay scoped to that one-shot pre-HEAD gate escalation and not become a per-block default. |
| `ValidateBorrowedFSPublicationAuthority` | `BlockAuthorityAdvisory` = LQ | REQUIRED: must stay off SERIAL on the publish hot path. |
| `ProbeBlockReuse` | session (prod LQ) | OBSERVED |
| `AddProvisionalBlockReferenceWithExpiry` | pinned producer (quorum write; see existing producer test) | OBSERVED renew; not Paxos |
| `AddPublishAttemptReferences` / stage | session LQ | OBSERVED |
| Repair INSERT/DELETE | ordinary session write, **no LWT** | OBSERVED; REQUIRED not to become per-block Paxos |
| `UpdateLibraryHead` CAS | `IF head_commit_id` + session `SerialConsistency` | REQUIRED: global serial domain for HEAD (do not design around `LOCAL_SERIAL`) |
| `confirmLibraryHeadCommitVisible` | `Consistency(SERIAL)` | OBSERVED v2 classify; candidate common classify step |
| Sync `updateLibraryHeadWithStats` CAS | same LWT, **no confirm** | OBSERVED split classifier |
| Repair HEAD read | SERIAL | OBSERVED cold path; one read under the shared 30-second classifier deadline |
| Repair parent walk | EACH_QUORUM | OBSERVED cold path; at most 1024 sequential reads under the shared 30-second deadline; one DC unavailable yields UNKNOWN/retain |
| `BlockHasReferencesGlobal` | EACH_QUORUM | **not** on publication hot path (GC) |
| `InitializeLibraryHeadIfUnset` (used by `InitializeLibraryFS` / `createInitialCommit`) | `IF head_commit_id = null AND created_at != null` + session `SerialConsistency` (since 2026-09-11; was a session `LOCAL_QUORUM` `LoggedBatch` with no LWT) | OBSERVED; pinned by `TestPC0RawHeadColumnWritersAreInventoried` / `TestPC0NoUnconditionalHeadUpdateRemains` |
| `CalculateLibraryStats` / `calculateDirStats` (v2 `UpdateLibraryHead`; Sync `commitTreeStats` ×2) | session reads, one per directory, recursive | OBSERVED cost inside the stage→HEAD window (§12); REQUIRED: not part of the coordinator's HEAD step as a full walk |

This PR must not add authority reads, CQL callsites, Paxos, or WAN
operations to production. R3 budgets remain the hot-path baseline.

---

## 9. Crash / ambiguity semantics

### v2 / SeafHTTP / OnlyOffice / cross-repo (`UpdateLibraryHead`)

1. LWT error that is not ambiguous → wrapped failure; callers typically do
   **not** treat it as conflict cleanup.
2. Ambiguous LWT → SERIAL confirm:
   - HEAD already `commitID` → treat **APPLIED** (`return nil`).
   - HEAD is something else → treat as failed update (not
     `ErrLibraryHeadConflict`); **no** conflict cleanup.
   - confirm error → `ErrLibraryHeadPublicationUnknown` (UNKNOWN, no
     conflict cleanup).
3. `applied=false` → `ErrLibraryHeadConflict` (KNOWN_LOSER) → request-local
   attempt cleanup (`CleanupFailedPublishAttempt`,
   `cleanupSeafHTTPFailedPublishAttempt`, or
   `cleanupOnlyOfficeFailedPublishAttempt`) which typically also clears the
   repair row. Crash before that cleanup is
   `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01`.

### Sync (`updateLibraryHeadWithStats`)

1. Any LWT error → `errSyncHeadCASUncertain` (UNKNOWN). Retain `pub:` and
   shared repair. HTTP 503 retry.
2. `applied=false` + `currentHead==targetHead` → idempotent success (another
   writer won the same target). Clean this attempt `pub:` only. **Do not**
   clear shared repair.
3. `applied=false` + divergent HEAD → retry / 503. Clean this `pub:`.
   Retain shared repair.
4. Post-CAS counter/derived failure → HEAD **did apply**; 503 pending
   reconciliation; do not unpublish.

### HEAD initializers (`InitializeLibraryFS`, `createInitialCommit`)

Since 2026-09-11 they publish through `InitializeLibraryHeadIfUnset`, which
classifies like the advance primitives: applied ⇒ this call initialized;
not applied with a head ⇒ adopt that head (losing commit row discarded);
not applied without a row ⇒ `ErrLibraryHeadNotFound`; ambiguous ⇒ SERIAL
confirm, else `ErrLibraryHeadPublicationUnknown`. At audit time no
classification existed: the write was an unconditional `LoggedBatch` and a
stale read simply won by timestamp (§3.4).

### Repair worker (shared)

Positive reachability only promotes. Everything else retains. The shared repair
classifier reads HEAD with SERIAL, walks at most 1024 parent rows sequentially
with EACH_QUORUM under one 30-second deadline, and maps missing/error,
timeout, cycle, malformed ancestry, natural genesis, bound exhaustion, or an
unavailable DC to UNKNOWN/retain. Timeout/lease is **not** cleanup authority
(closed `ISSUE-PUBLISH-REPAIR-TIMEOUT-CLEANUP-01`). The reachability issue is
closed for this shared classifier; broader R31 convergence remains open.
Convergence is a separate, open problem: the walk is bounded at 1024 nodes
**from the current HEAD**, so once HEAD has advanced more than 1024 commits
past the target (an active library during a multi-hour retry window — retry
backoff reaches 6 h and one unavailable DC yields UNKNOWN), the target can
never be classified again while `pub:` still expires at 35 d. Retain stays
correct; UNKNOWN may simply never converge
(`ISSUE-PUBLISH-REPAIR-REACHABILITY-CONVERGENCE-01`, P1, PRE-X1 / R31; not a
#213 regression and not fixed here).

---

## 10. Common vs funnel-specific

### Belongs in a future coordinator (universal kernel)

```text
accept PublishableInput only
  (OWNED with own liveness, or BORROWED after acquiring durable own up:,
   capturing ExpectedP when required; observing foreign fs: without an
   own pin is not enough)
stage attempt-local pub:
durable repair intent for every block dependency
  (an empty-file path with no physical dependencies may have no repair row)
publication readiness when this funnel has it
  (also before HEAD; relative order is funnel-specific;
   the FINAL exact-P revalidation against ExpectedP, when required,
   happens here -- not before staging)
HEAD attempt + classify APPLIED | KNOWN_LOSER | UNKNOWN
settlement:
  APPLIED → promote fs: / clear repair on success
  KNOWN_LOSER → exact attempt cleanup (and only then)
  UNKNOWN → retain
```

`UNPROVENANCED` and `ERROR` never enter this kernel; rejecting them does not
produce `PublishableInput`. Adapters must prove/renew own liveness for
`OWNED`, or acquire durable own liveness for `BORROWED` and capture
`ExpectedP` before a target coordinator stages. The current F3 sequence is
`verify/classify → capture commitBlockPlacement(ExpectedP) for every ready
SessionUpload or BorrowedFS block → own up: → stage → repair → final exact-P
revalidation → HEAD`; the adapter step captures observed placement and
acquires the pin, **not** the final exact-P check. Doing that final check
before staging would reopen the W1 TOCTOU.
Today's Sync can still publish after a clean global miss; the target
coordinator must reject that unprovenanced input. Centralizing it without
it only centralizes the hole. Cross-repo still publishes borrowed source
`fs:` without a destination own pin — that is today's gap, not a permitted
`PublishableInput` shape.

This kernel only ever stages `PublishableInput` for dependencies the HEAD
being published will *newly* live on. It does not revisit dependencies the
new HEAD inherits unchanged from the old HEAD, even when that inherited
dependency's own continuity was `CONDITIONAL`/`UNKNOWN` when it first
entered an earlier HEAD (`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`,
tracking R3's own `LogicalPositiveBlockDelta` caveat). The current GC cannot
be assumed to make that safe: Phase 5's expired-version cascade deletes
fs_objects still reachable from HEAD (§6 PUBL-10, §15) — reachability is
enforced at scan time in Phase 6 and not at all in Phase 5's commit cascade.
The decision that must be made with evidence before PC-2 migrates any funnel
is therefore between (a) a *repaired, sharing-aware* GC assuming
responsibility for inherited dependencies and (b) widening the coordinator's
work set to include inherited dependencies whose continuity was never proven,
per R3's caveat. This boundary's silence decides neither. PC-1 itself is
skeleton/common types only, behavior-preserving, zero funnels migrated; it
does not need to (and must not) freeze full-work-set semantics by
implication, but PC-2 does pick a concrete `PublishableInput` shape and
cannot do that correctly until that decision is made.

Two more prerequisites sit outside the kernel and must not be absorbed into
it as flags: HEAD initialization had to move into the CAS domain (§3.4; done
2026-09-11), and content resurrection (§3.5) must become an adapter rather
than stay "tree-only".

### Belongs in adapters (must not become coordinator flags)

| Adapter | Owns |
|---|---|
| Sync | PutBlock, RecvFS, PutCommit, CheckBlocks, parent/ancestry, auto-merge, stats/counters, `up:sync:<repo>:<block>` evidence |
| SeafHTTP | Seafile upload protocol, fs_object layout, commit INSERT |
| V2 stored upload | upload-link, chunk assemble, `UploadFile` |
| CreateFileFromBlocks | session, `/blocks/check`, BorrowedFS classify, session claim LWT |
| OnlyOffice | callback download, pending-block row |
| Cross-repo | copy fs_objects, conflict policy, source HEAD |
| CreateFile | template materialize / empty file |
| Content resurrection (R1–R4) | history/trash lookup, resurrected fs_object → durable own pin on its blocks + `ExpectedP` before stage (today: nothing) |

Rejected coordinator shape:

```text
PublicationCoordinator(isSync, isOnlyOffice, borrowed, autoMerge, ...)
```

Also rejected: one elected leader, global library lock, SERIAL per block by
default, EACH_QUORUM on every normal op, "coordinator instance in dc-na".

---

## 11. Sync PutBlock → HEAD identity

**Question:** is there a durable identity that binds a PutBlock to a future HEAD?

| Candidate | Exists? | Unique per publication? | Survives retry/pod/DC? | Written before dependence? | Distinguishes PutBlock vs dedup? |
|---|---|---|---|---|---|
| Commit ID | the `commits` row exists at `PutCommit`, which the Seafile client sends **before** fs objects and blocks; HEAD moves later via `update-branch` / `PUT /commit/HEAD` | per commit, not per PutBlock | yes if committed | the row exists before the blocks are PUT, but **no PutBlock is written to it** — `PutBlock` carries only `(repo, block)` | no |
| fs ID | at RecvFS | per file object | yes | not at PutBlock | no |
| repo ID | yes | no | yes | yes | no |
| `up:sync:<repo>:<block>` | yes | **no** — deterministic per (repo, block) | row survives; visible at `LOCAL_QUORUM` same-DC, and cross-DC via the `EACH_QUORUM` fallback on a clean local miss (#210, resolved) | yes at PutBlock | **no** — same key for real PUT and later renew; absent after TTL or CheckBlocks skip |
| Upload token | auth only | no | session | n/a | no |
| Request identity | no durable | n/a | no | n/a | no |
| Server session | none for Sync blocks | n/a | n/a | n/a | no |
| Client protocol metadata | block id + bytes | content hash | n/a | n/a | CheckBlocks is the opposite of PutBlock |

**Finding (must not be softened):**

```text
current Sync evidence fundamentally requires inference
```

The only durable PutBlock residue is the deterministic `up:` referrer. It is
not a publication attempt id. #210 changed its *visibility domain* (a clean
local miss now escalates to `EACH_QUORUM` before being treated as absence),
not its identity: the fallback still infers presence/absence from the same
`up:sync:<repo>:<block>` key, still cannot distinguish a real PutBlock from a
later renewal, and still goes absent past its 48h TTL
(`ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01`) or when `CheckBlocks` skipped
PutBlock entirely. The finding above is unchanged by #210.

The 2026-09-10 audit corrected this section's
earlier wording that the commit only appears at HEAD time: `PutCommit` stores
the commit row first, so a *pending commit* exists server-side while blocks
arrive. That does **not** yet give PutBlock a durable, unambiguous binding —
retries, concurrent uploads from the same client, dedup, and auto-merge all
need a demonstrated association before "pending commit" can be used as
identity. Recorded as design material, not adopted:

- **DESIGN HYPOTHESIS (follow-up):** bind PutBlock to the pending commit(s)
  recorded by PutCommit for the same repo/token.
- **DESIGN OPTION (follow-up):** have `CheckBlocks` pin (fenced) the blocks
  it reports as existing, converting the dedup path into a provenanced one —
  O(N) writes with TTL, retry, and GC-race semantics that must be designed.

Neither changes the plan to migrate the Sync adapter last.

**Re-characterized after rebase** (this baseline now contains #210): the
cross-DC blind spot this section originally flagged is closed — a PutBlock
acknowledged in `dc-eu` is now recoverable from a request handled in `dc-na`
before hints/repair land, with real 3-DC evidence
(`scripts/w2-sync-putblock-xdc-provenance-validation.sh`,
`internal/integration/sync_w2_putblock_xdc_provenance_multidc_test.go`; see
§13 M2). The trade-off: a commit made entirely of genuinely-unprovenanced
(dedup-only) blocks now shares the same `EACH_QUORUM` availability
dependency, so one datacenter down can newly block that commit's HEAD publish
where it previously could not
(`ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01`, accepted trade-off,
decision recorded 2026-09-09). This is an availability-domain change, not an
identity change.

---

## 12. Cost / hot-path

No production cost is added by this PR.

**Correction (2026-09-10):** the table originally omitted the per-publish
tree-stats walk. Every v2/SeafHTTP/OnlyOffice/cross-repo/resurrection publish
pays `UpdateLibraryHead → CalculateLibraryStats → calculateDirStats`: one
session-consistency `fs_objects` read **per directory** of the new tree,
recursively, with no cache, *inside* the stage→HEAD window (after `pub:` and
repair are written, before the CAS). Sync pays it **twice** before its CAS
(`commitTreeStats(previous)` + `commitTreeStatsStrict(new)` in
`updateLibraryHeadWithStats`). For a library with D directories that is
O(D) sequential reads per publish (2·O(D) for Sync), lengthening the
`pub:`-only window and the LWT contention window under concurrency. Rows
marked `O(1)` below are O(1) in *mutation* work only; add the stats walk to
every row. Recorded as `ISSUE-PUBLISH-HEAD-TREE-STATS-COST-01` (P2,
FOLLOW-UP); not optimized here.

| Funnel | DB shape (+ stats walk O(D), see above) | Local | Cross-DC / WAN | Paxos |
|---|---|---|---|---|
| CreateFile (empty) | O(1) tree + O(D) stats walk + HEAD | tree reads | HEAD | 1 HEAD LWT |
| CreateFile (template) | + materialize | LQ pin/fence/install | HEAD; install LWT if new | HEAD + possible install |
| UploadFile stored | O(blocks) stage | LQ | HEAD | HEAD |
| CreateFileFromBlocks | O(blocks) renew+fence + session claim + optional successful slot release | LQ × N + quorum upserts | session-claim LWT + HEAD LWT under the configured serial domains; no added EQ on the normal path | session claim + HEAD; full successful requests also include the slot-release LWT when the cap is enabled |
| OnlyOffice | O(1) + O(D) stats walk | LQ | HEAD | HEAD |
| SeafHTTP | O(blocks) stage + 1 commit INSERT | LQ | HEAD | HEAD |
| Cross-repo | O(copied files/blocks) | LQ | dest HEAD (+ source HEAD) | dest/source HEAD |
| Sync scope gate (N distinct candidates) | O(N) LQ scope reads | LQ × N | 1 EACH_QUORUM for each clean LQ miss; LQ errors abort without EQ | none beyond HEAD |
| Sync provenanced subset (P candidates) | remaining readiness/renewal for P after scope gate | LQ/EQ scope gate + funnel-specific readiness | repair cold SERIAL/EQ; HEAD | HEAD only |
| Sync unprovenanced subset (U global misses) | stage/repair/HEAD, no Sync readiness for those blocks | LQ/EQ scope gate still paid | no per-block readiness; HEAD | HEAD |
| Sync HEAD step (any subset) | 2·O(D) stats walks before the CAS | session reads | HEAD | HEAD |
| Content resurrection R1–R4 | history/trash read + O(1) tree + O(D) stats walk + HEAD; **no** liveness work | tree reads | HEAD | HEAD |

The CreateFileFromBlocks row has two useful scopes. Before HEAD, the path
pays one session-claim LWT and one HEAD LWT, in addition to its bounded
per-block quorum upserts and advisory exact-P reads. A successful request also
calls `CleanupCommittedBlockUploadSessionCaps`; when the staging cap is
enabled, that conditional slot-release is another LWT after the idempotency
result is recorded. It is not part of the pre-HEAD protocol cost.

Only a **clean** local miss escalates to EACH_QUORUM. A local read **error**
fails closed without escalation; an EQ error fails closed after the EQ attempt
and before repair/HEAD (§7, PUBL-7). The clean-local-miss fallback is the
availability/cost trade-off #210 introduced; the clean-global-miss path is a
separate W2 gap and must not be folded into a flat "Sync readiness" line.

Local fast path that **must** be preserved: LQ presence of own `up:` and LQ
exact-P **after** a durable own pin. Do not put EACH_QUORUM or SERIAL on that
path by default.

R3 declared exception for Sync readiness remains the only accepted O(N)
authority-shaped publish cost. PC-0 does not raise that budget.

The shared repair cold path is bounded separately from the writer hot path: one
SERIAL HEAD read plus at most 1024 sequential EACH_QUORUM parent reads under a
30-second total context. This is the narrow shared-classifier closure from #213;
it does not close broader R31 lifecycle, discovery, or other-funnel obligations.

---

## 13. 3-DC topology + characterization matrix

Reuse, do not duplicate:

- `scripts/x2-multidc-validation.sh` — GC EACH_QUORUM / topology
- `scripts/w2-post-head-multidc-validation.sh` — local blindness cannot cleanup
  a publication made in another DC
- `scripts/w2-sync-putblock-xdc-provenance-validation.sh` /
  `internal/integration/sync_w2_putblock_xdc_provenance_multidc_test.go` —
  Sync pre-HEAD scope-gate cross-DC recovery (#210, resolved 2026-09-08,
  now in this baseline); reuse for M2, do not duplicate

Gate: `SESAMEFS_REQUIRE_PC0_PUBLICATION_CHARACTERIZATION=1`.
Skip under that gate is FAIL. Missing fixture is FAIL. Required named **matrix
rows** must be recorded. Default Docker `go-all-test` does **not** inherit
this gate.

This is a **3-DC topology + characterization matrix** gate, not an evidence
runner. When armed it connects to `dc-na`/`dc-eu`/`dc-asia` and records the
matrix. It does **not** re-run `w2-post-head-multidc-validation.sh` or
`x2-multidc-validation.sh`, and it does **not** execute publication races
M1–M9. Rows marked prior evidence cite those scripts; they are not new
proofs from this PR. `UNKNOWN` / `GAP` / `PRIOR-EVIDENCE-NOT-RERUN` may
make the matrix complete. Reserve `executed evidence` / `leg executed` for
scenarios that actually ran a publication race.

Harness prints the recorded matrix rows. UNKNOWN is explicit, never silent
green.

| Row | Claim | Result on this baseline |
|---|---|---|
| M1 Local fast path | LQ presence / local tree / exact-P / stage writes stay local; funnel-specific LWTs remain serial-domain coordination | **OBSERVED** (source). Live 3-DC not required to see there is no EQ on those operations. |
| M2 Remote provenance before repair | PutBlock in dc-eu, HEAD in dc-na before hints | **PRIOR EVIDENCE** (`scripts/w2-sync-putblock-xdc-provenance-validation.sh`, #210 resolved 2026-09-08, real 3-DC RED→GREEN). Not re-executed by PC-0's own gate. A clean local miss now escalates to `EACH_QUORUM` before being treated as absence; a genuine global miss still skips W2 readiness for that block (separate, already-tracked gap, not what #210 closed). |
| M3 One DC down | EQ/SERIAL ops fail closed | **MIXED / PRIOR EVIDENCE — PARTIAL**: #213's shared repair classifier proves the cold-path SERIAL HEAD/EACH_QUORUM ancestry boundary fails closed and retains repair when one DC is unavailable; #210 separately proves the Sync scope-gate EACH_QUORUM fallback fails closed. Publication HEAD (`SERIAL`) can still proceed if its serial domain remains available — **not re-measured here**. Funnel-complete M3 (every funnel, every EQ/SERIAL primitive) = still GAP. |
| M4 Cross-DC HEAD settlement | attempt in eu, repair in na | **PRIOR EVIDENCE — PARTIAL**: #213's shared classifier recognizes a target as an ancestor after HEAD advances and retains repair when one DC is unavailable; `scripts/w2-post-head-multidc-validation.sh` also proves cross-DC HEAD blindness does not authorize cleanup. Full remote replay/settlement, especially Sync-specific M4, remains **GAP** and was not re-executed by PC-0. |
| M5 Concurrent publishers | writer A na, writer B eu | CAS winner is Paxos-level **OBSERVED** (single-cluster tests). Live two-DC concurrent publishers = GAP. |
| M6 Cross-DC repair | pub/repair from one DC, worker in another | **EVIDENCE GAP** for the concrete DC-A write → DC-B discovery → settlement-worker proof; the W2 script's local-miss-not-cleanup observation is not that end-to-end proof, and PC-0 did not re-execute it. |
| M7 Stale placement | P changes before pre-HEAD fence | **MIXED**: F3 exact-P fence is **OBSERVED** (W1 retired-placement); Sync's provenanced subset has source/existing evidence for final exact-P validation; remaining funnels have no pre-HEAD exact-P fence = **GAP**. |
| M8 Funnel-specific | Sync, CFFB, stored v2, SeafHTTP, OO, cross-repo | **MIXED/PARTIAL**: CFFB/shared has classifier evidence, not full end-to-end multi-DC funnel proof; Sync xDC is **PRIOR EVIDENCE** (#210, see M2/M3); full 3-DC proof for OO/SeafHTTP/cross-repo remains **EVIDENCE GAP**. |
| M9 Initial HEAD from a blind DC | initializer vs CAS-published HEAD | **PRIOR EVIDENCE, resolved**: audit 2026-09-10 (`scripts/pc0-initial-head-xdc-probe.sh` bug mode, real 3-DC) — the pre-fix `createInitialCommit`/`InitializeLibraryFS` shape from blind dc-eu reverted an LWT-published HEAD in every DC; 2026-09-11 (`scripts/h1-initial-head-multidc-validation.sh`, real 3-DC, handler-level) — both production initializers driven from blind dc-eu keep and return the HEAD dc-na published. Not re-executed by the gate. |

Audit re-execution note (2026-09-10): the #210 and #213 scripts cited as
prior evidence were re-run on this baseline. `w2-sync-putblock-xdc-provenance-validation.sh`
passed 4/4 on its second run (first run aborted in the N=1000 cost leg
right after node restarts; because of `set -e` the fail-closed leg did not
run). `w2-post-head-multidc-validation.sh` passed 6/6 on its third run (two
runs failed the EACH_QUORUM seed with `received only 2 responses` seconds
after `migrate`; the same seed passes in 0.03 s from a warm runner). Harness
startup sensitivity is tech debt, not an architectural result (§15).

The table is a characterization matrix. Completeness means every row has a
status string, not that M1–M9 ran as publication races (M9 is a separate
cqlsh probe, `scripts/pc0-initial-head-xdc-probe.sh`, not executed by the gate).

---

## 14. Architectural conclusion

### Verdict

```text
PROCEED WITH COORDINATOR
```

### Why the idea was not refuted

The inventory supports two different statements; they must not be collapsed.

#### Observed common kernel today

There is no universal `classified → publishable → stage` order in the current
funnels. For every block-bearing publication, the stable observed kernel begins
once a funnel stages:

```text
                 ┌→ durable repair intent ──┐
stage pub ────────┤                           ├→ HEAD
                 └→ readiness/final exact-P ┘
                    (when present; also before HEAD; relative order is funnel-specific)
```

An empty-file path with no physical dependencies may have no repair row. CFFB
observes `verify/capture placement → own-liveness work → stage → repair →
final exact-P fence → HEAD`; Sync observes `stage → provenance/readiness →
repair → HEAD`, and can discover `UNPROVENANCED`/`ERROR` after staging. The
other current block-bearing funnels observe `stage → repair → HEAD`. The target
coordinator contract below deliberately adds a stronger adapter boundary to
close the classification gap; it is not a claim about every current funnel.

#### Target coordinator contract

```text
adapter classification/evidence
→ PublishableInput (target invariant: no UNPROVENANCED/ERROR)
→ PublicationCoordinator
```

Differences are:

1. **Which adapter proves blocks** (session, PutBlock inference, OO download,
   copy) and which classes remain `UNPROVENANCED`/`ERROR`. The target
   coordinator must reject those before `stage pub:`; current Sync can stage
   before its scope gate discovers that outcome.
2. **Which funnels omit publication-authority/continuity at HEAD** (gap by
   provenance, not a second protocol). Exact-P is one mechanism; renewal/
   overlap can close own-`up:` materialization. The target adapter must acquire
   durable own `up:` for `BORROWED` and carry `ExpectedP` where applicable;
   the final exact-P revalidation remains after stage and before HEAD, with its
   relation to repair kept funnel-specific.
   F3 carries `ExpectedP` for both `SessionUpload` and `BorrowedFS`; the
   adapter-specific order between capture and pin acquisition is not frozen.
   Cross-repo still needs a destination own pin.
3. **Two HEAD classifiers** (v2 SERIAL confirm vs Sync uncertain-on-any-error).
4. **Repair ownership** (unique attempt commit vs shared Sync `commit_id`).
5. **Repair vs readiness order** (CFFB repair-then-fence vs Sync
   readiness-then-repair). Do not unify this as a silent PC-1/PC-2 default.

Those are extraction and migration problems, not evidence that a common
protocol does not exist. Centralizing the spine would *reduce* W2 surface
once publishable input is required and UNKNOWN/loser rules stop being
re-proven per funnel.

The 2026-09-10 audit strengthened, not weakened, this conclusion: the act of
enumerating every path that changes block liveness through HEAD surfaced two
HEAD writers outside the CAS model (§3.4), four resurrection paths that the
"tree-only" label had hidden (§3.5), and the GC negative side interacting
with shared fs_objects (§6 PUBL-10). A coordinator is the place where such
paths become impossible to hide; the characterization is what found them.

### Why not "implement it in this PR"

The plan forbids it. Also: Sync identity is still inference (§11, unchanged
by #210); publication authority/continuity is not universal
(`ISSUE-PC0-EXACT-P-FUNNEL-GAP-01`); the coordinator boundary's scope to
newly-live dependencies is not yet justified against R3's inherited-work-set
caveat (`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`); unifying HEAD
classify or readiness/repair order is a behavior-sensitive change that needs
its own PR.

### Candidate boundary (CANDIDATE / NOT IMPLEMENTED)

```text
          FUNNEL-SPECIFIC

SyncEvidenceProvider
SeafHTTPEvidenceProvider
V2UploadEvidenceProvider
CreateFileFromBlocksEvidenceProvider
OnlyOfficeEvidenceProvider
CrossRepoEvidenceProvider
          │
          ▼
   ClassifiedPublicationInput
     OWNED | BORROWED | UNPROVENANCED | ERROR
          │
          ▼
   OWNED: prove / renew own liveness
    BORROWED / provenance requiring ExpectedP:
      { capture ExpectedP where required;
        acquire or renew durable own up: }
      (the order between these adapter steps is adapter-specific;
       both precede target staging; this is NOT the final check --
       foreign fs: observation alone, without the own pin, is not publishable)
   reject ERROR            ── does not produce PublishableInput
   resolve or reject UNPROVENANCED
          │
          ▼
   PublishableInput
     - canonical blocks that are actually publishable
     - durable own liveness for every physical dependency
      - ExpectedP where the adapter requires it (F3: SessionUpload or BorrowedFS;
        captured, not yet revalidated)
     - attempt identity (commit/attempt id)
          │
          ▼
       COMMON  (stateless orchestration on every node)

 PublicationCoordinator
          │
          ├─ stage liveness          (PublishableInput only)
          ├─ repair durable          (for every block dependency; an empty-file
          │                            path may have no row)
          ├─ publication readiness   (optional; renew own liveness and/or FINAL
          │  revalidation against ExpectedP -- after stage and before HEAD;
          │  its order relative to repair is funnel-specific)
          ├─ HEAD attempt + classify
          └─ settlement
```

Do not freeze Go APIs. The names above are documentation. For CFFB, PC-2
must preserve `stage < repair < final exact-P revalidation < HEAD` (§4, §5
F3); `PublishableInput` carrying `ExpectedP` is not itself proof the
revalidation happened.

### Coordinator must be multi-DC from PC-1

Valid:

```text
attempt begins in dc-eu → process dies → repair continues in dc-na
→ dc-asia has a stale LQ view → system still converges
```

Invalid: home-DC coordinator, in-memory lock, "not visible locally ⇒ absent".

Durable coordination remains Cassandra + appropriate CL/LWT domains. HEAD
itself advances **only** through that domain: the unconditional initializers
(§3.4) violated this at audit time and were converted on 2026-09-11, outside
the coordinator, before PC-1.

### Relation to W2 / R31 / G3 / G4 / #209 / #210 / #212 / #213

- **Coordinator ≠ W2 closed.** W2 still must prove: once D(P1) committed, no
  legitimate writer publishes durable liveness on P1.
- **Coordinator ≠ R31 closed.** Possibly-applied HEAD must not lose
  definitive liveness because `pub:` TTL expired.
- **G4** allows orphan COMMITTED(P1,D1) + blocks(L)=P2. Do not do G4 until
  every physical dependency reaches HEAD with continuous valid publication
  authority/liveness for its provenance (own pin + exact-P, renewal/overlap,
  or both). Do not freeze "every funnel must obey exact-P" as that G4
  condition. This characterization is a prerequisite for that uniformity,
  not a G3 blocker.
- **GC Phase 5 (negative side).** `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01`
  (P0 latent, PRE-GC): the expired-version cascade deletes fs_objects still
  reachable from HEAD. Not a coordinator concern to fix, but it removes "GC
  already protects inherited dependencies" from the option list and must be
  fixed before any `GC_ENABLED=true` with `version_ttl_days > 0`.
- **G3 is implemented by #212.** The GC-side canonical-retirement change is already in this baseline and is orthogonal to the publication funnels; no PC-0 funnel re-characterization is required.
- **#209/#210/#212/#213:** done in their narrow scopes. This branch is rebased onto
  `main` at `7b9102af9` (§ baseline note). Sync M2 / LQ-miss→EQ-fallback / the
  consistency map (§7, §8) / §11 / §13 have been re-characterized against
  #210's merged `BlockReferenceExistsEachQuorum` fallback and its real 3-DC
  evidence; the known issue it closed
  (`ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01`) is resolved, not
  reimplemented here. #213's shared repair classifier is also represented in
  §7–§9 and M3/M4: SERIAL HEAD, bounded 1024-node EACH_QUORUM ancestry, a
  30-second deadline, and UNKNOWN-retain on inconclusive evidence. That does
  not close broader R31, OnlyOffice's independent legacy traversal, Sync
  expired provenance, or M6's cross-DC discovery/settlement proof. #209 (G2
  PREPARED→COMMITTED handoff) touches only `internal/gc` and needed no further
  publication-funnel re-characterization. #212 (G3 canonical retirement) is GC-side
  and likewise required no further publication-funnel re-characterization. Source contracts were rerun after
  the rebase (see below).

### Recommended next PR sequence (not frozen, not implemented)

```text
PC-0  this PR (characterization)
  → H1 follow-up (separate, small, prioritized): conditional HEAD initializer
     — coordinator prerequisite (§3.4) — DONE 2026-09-11
  → PC-1  coordinator skeleton / common types, behavior-preserving, zero funnels migrated;
          does not freeze full-work-set semantics
  → resolve ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01 with evidence, before PC-2
     (GC Phase 5 fix is PRE-GC regardless; R31 convergence is PRE-X1)
  → PC-2  migrate the best-understood funnel (CreateFileFromBlocks / shared Once)
          preserving today's stage < repair < final exact-P revalidation < HEAD
          order unless a separate PR unifies it with evidence
  → PC-3… remaining v2/SeafHTTP/OnlyOffice/cross-repo, and the content-resurrection
     adapter (R1–R4) as BORROWED with a real pin
  → Sync adapter last
  → only then consider a Sync provenance redesign if identity remains inference
```

If PC-1 cannot unify HEAD classify without a behavior change, that change is a
**separate** explicit PR, not a silent extra in PC-1.

---

## 15. Findings (not fixed here)

| ID | Sev | Scope | Finding |
|---|---|---|---|
| `ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01` | P1 | FOLLOW-UP / W2 (newly registered by PC-0, not introduced by it) | `PublishableInput`/the candidate coordinator boundary only cover dependencies a HEAD will *newly* live on. R3's own `LogicalPositiveBlockDelta` note says that delta is not the complete work set: dependencies inherited unchanged from the old HEAD, whose continuity was `CONDITIONAL`/`UNKNOWN` when first proven, are not covered. The current GC is not a defense (Phase 5 counterexample below, PUBL-10); the decision for PC-2 is between a repaired, sharing-aware GC taking that role and widening the work set (§2, §6, §10). |
| `ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01` (multi-DC reversion variant) | P1 → **resolved 2026-09-11** | was FOLLOW-UP, separate and prioritized; coordinator prerequisite (pre-existing) | Two unconditional `UPDATE libraries SET head_commit_id` initializers (`InitializeLibraryFS`, `createInitialCommit`, the latter reachable from `GET /commit/HEAD`) lived outside the CAS domain; reproduced on the real 3-DC fixture reverting an LWT-published HEAD from a blind DC (§3.4, M9). Fixed by `InitializeLibraryHeadIfUnset` with unit, single-cluster and handler-level 3-DC evidence; `TestPC0NoUnconditionalHeadUpdateRemains` + mutation leg M10 pin it. |
| `ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` | P1 | FOLLOW-UP / W2 / funnel migration (pre-existing, newly classified) | `RevertFile`, `RevertDirectory`, `RestoreTrashItem`, `RevertDirents` publish a positive borrowed block-dependency delta with no pin, `pub:`, repair, or fence (§3.5). Reclassified from tree-only; not fixed here. |
| `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` | **P0 latent** | PRE-GC runtime (pre-existing; discovered by PC-0's inherited-dependency question) | Phase 5's expired-version cascade deletes content-addressed fs_objects and their `fs:` references while HEAD still depends on them; no keep-set, and `acquireLibraryDeleteGuard` is effectively a no-op for these items. `TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD` freezes the observed behavior. Dormant only while `GC_ENABLED=false`. Not fixed here. |
| `ISSUE-PUBLISH-REPAIR-REACHABILITY-CONVERGENCE-01` | P1 | PRE-X1 / PRE-GC / R31 convergence (pre-existing; not a #213 regression, not a #211 blocker) | Bounded 1024-node walk from the *current* HEAD plus EACH_QUORUM parent reads: an active library during a multi-hour retry window (one DC down ⇒ UNKNOWN, backoff to 6 h) makes the target permanently unclassifiable while `pub:` still expires at 35 d. Retain remains correct; UNKNOWN may never converge (§9). |
| `ISSUE-PUBLISH-HEAD-TREE-STATS-COST-01` | P2 | FOLLOW-UP (cost) | Every publish pays a recursive per-directory stats walk inside the stage→HEAD window; Sync pays two before its CAS. §12 corrected; not optimized here. |
| Raw-CQL HEAD writers invisible to the lexical guard | P2 | THIS-PR (hardening, closed) | `TestPC0RawHeadColumnWritersAreInventoried` + mutation leg M7; the finding is not hypothetical (§3.4). Method-value / aliased-callee coverage stays documented as out of scope: P2 TECH DEBT. |
| §11 protocol-order wording | P2 | THIS-PR (fixed) | `PutCommit` stores the commit before blocks arrive; PutBlock↔pending-commit binding is a DESIGN HYPOTHESIS and `CheckBlocks` pins a DESIGN OPTION, both follow-ups, neither adopted. Sync remains last. |
| Harness startup sensitivity | P2 | TECH DEBT | #210/#213 scripts abort on non-evidence legs (cost) or EACH_QUORUM timeouts seconds after `migrate`/node restarts; integration runs against the 3-DC fixture need `CASSANDRA_HOSTS` pointed at a fixture node or `TestMain` cleanup fails against the dev Cassandra (§13, `docs/TESTING.md`). No architectural impact. |
| `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` | P1 | FOLLOW-UP / W2 (newly registered by PC-0, not introduced by it) | Publication-authority/continuity before HEAD is not uniform by provenance. Exact-P exists only for CreateFileFromBlocks placements and Sync-provenanced blocks. `UploadFile` passes `nil` into the shared finalizer. CreateFile, OnlyOffice, SeafHTTP, cross-repo have no pre-HEAD fence. This does **not** prescribe exact-P as the only fix. |
| Sync PutBlock identity | P1 | already `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01`; cross-DC visibility slice closed by `ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01` (#210, resolved) | Evidence is still inference from `up:sync:<repo>:<block>` — #210 widened its visibility domain, not its identity (§11). The target coordinator must reject input with no provenanced PutBlock; today's Sync can still publish after a clean global miss, which remains the open W2 gap. |
| HEAD classify split | P2 | FOLLOW-UP / PC-1 | v2 confirms ambiguous CAS with SERIAL; Sync maps every CAS error to UNKNOWN without confirm. |
| Cross-repo own liveness | P1 | already R3 `UNKNOWN` | Destination does not take own `up:`. Exact-P alone would still be TOCTOU. |
| Known-loser durability | P2 | already `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01` | No durable loser witness. |
| Repair reachability | P1 (closed narrow scope) | `ISSUE-PUBLISH-REPAIR-REACHABILITY-01` closed for the shared classifier by #213; broader R31 remains open | Shared cold path is bounded to one SERIAL HEAD read plus at most 1024 sequential EACH_QUORUM parent reads under 30 seconds; inconclusive evidence retains repair. |
| `pub:` TTL | P1 | already R31 / `ISSUE-GC-PUB-REF-ZERO-REF-01` | Finite TTL can still open a liveness gap. |
| M3/M4/M5/M8 3-DC (remaining) | — | MATRIX GAP | M2/M3/M8's Sync-specific slice now has prior evidence (#210, §13). Still GAP: M3's funnel-complete claim (every funnel, every EQ/SERIAL primitive), M4's Sync-specific slice, M5 (live two-DC concurrent publishers), and M8's OO/SeafHTTP/cross-repo slice. |

W2, R31, and X1 remain OPEN.

---

## 16. Tests in this PR

| Test | Property |
|---|---|
| `TestPC0AllHeadCallersAreInventoried` | functions and package-level `var = func` literals that lexically call named HEAD helpers must be classified; method values/aliases/second branches remain out of scope |
| `TestPC0BlockPublicationFunnelsHaveMappedSeams` | each publication funnel has characteristic seams/stage/repair/HEAD/settle symbols; characteristic seams are not assumed to be a universal pre-stage phase |
| `TestPC0R3StageToHeadInventoryIsSubset` | live R3 `r3PublicationStageToHeadBoundaries` labels are a subset of the PC-0 mapping |
| `TestPC0BlockBearingFunnelsKeepDurableRepairBeforeHEAD` | every mapped block-bearing funnel keeps `stage < durable repair < HEAD`; an empty-file branch may produce no repair row |
| `TestPC0ObservedRepairReadinessPartialOrder` | CFFB `stage < repair < fence < HEAD`; Sync `stage < readiness < repair < HEAD`; auto-merge caller `stage < helper < HEAD` |
| `TestPC0CriticalConsistencyPrimitivesArePinned` | selected source tokens at named primitives; not the full consistency map; HEAD serial domain is not pinned |
| `TestPC0PublicationCoordinatorTypeIsNotImplemented` | no `PublicationCoordinator` type declaration (struct, interface, alias, or generic) anywhere under `internal/`, via an AST walk of every top-level `*ast.TypeSpec`, not a literal-string scan of three fixed directories |
| `TestPC0PublicationWrappersRemainAliases` | CreateFileFromBlocks/UploadFile/SeafHTTP wrappers still delegate |
| `TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams` | tree-only HEAD callers do not invoke the known block-publication stage seams |
| `TestPC0StoredUploadExactPFenceIsNoOpWhenCommitBlocksNil` | UploadFile's nil `commitBlocks` path keeps the exact-P fence a no-op |
| `TestPC0RawHeadColumnWritersAreInventoried` | every production string literal (internal/, cmd/) writing `libraries.head_commit_id` is inventoried with its shape (`cas` / `insert-create` / `update-unconditional`); closes the raw-CQL blind spot; flipping an initializer to CAS must flip its shape |
| `TestPC0ContentResurrectionPathsObservedWithoutPublicationSeams` | R1–R4 are inventoried as content resurrection and today call no stage/repair/fence seam; migrating one forces reclassification |
| gc `TestPC0Characterization_Phase5CascadeRemovesFSObjectsSharedWithHEAD` | executable counterexample for PUBL-10 / `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01`; freezes the observed unsafe cascade and must be inverted when Phase 5 becomes sharing-aware |
| `scripts/pc0-initial-head-xdc-probe.sh` | real 3-DC probe of §3.4 with two fail-closed modes: bug mode runs the pre-fix unconditional shape from a blind DC and requires HEAD reverted (the record of the bug); `--expect-cas-fix` runs the production conditional shape (`IF head_commit_id = null AND created_at != null`) from the same blind DC and requires it rejected and HEAD survived; the CAS control leg asserts `[applied]=False` and the real HEAD. CQL-shape level |
| `scripts/h1-initial-head-multidc-validation.sh` + `TestH1InitialHead*3DC` | real 3-DC, handler-level, one dc-eu stop/restart cycle per initializer on its own library: `InitializeLibraryFS` and Sync `createInitialCommit`, each driven from a DC asserted blind immediately before it runs, keep and return the HEAD another DC published, and that HEAD's commit is servable locally at the consistency `GET /commit/:id` uses; gate `SESAMEFS_REQUIRE_H1_INITIAL_HEAD_MULTIDC_EVIDENCE=1`; RED against the pre-fix production files |
| `TestPC0NoUnconditionalHeadUpdateRemains` + clause pins | no production UPDATE of `libraries.head_commit_id` without `IF`; `TestPC0CriticalConsistencyPrimitivesArePinned` pins `IF head_commit_id = null` and `AND created_at != null` independently; mutation legs M10/M10a/M10b require RED |
| integration `TestPC0PublicationMultiDCCharacterization` | 3-DC topology + matrix rows; gate cannot skip-green; GAP/UNKNOWN may complete the matrix |
| `scripts/pc0-publication-inventory-mutation-validation.sh` | 12/12 mutation legs RED: M1 untracked named publisher; M2 untracked package-level `var = func` publisher; M3 parenthesized package-level function-valued publisher; M4 missing funnel seam; M5 tree mutation invoking a publication stage; M6 CL token downgrade; M7 raw-CQL `head_commit_id` writer outside the allowlist; M8 resurrection path invoking a publication stage; M9 exact-P fence moved before stage; M10 initializer stripped of its whole `IF` condition; M10a initializer loses `head_commit_id = null`; M10b initializer loses `created_at != null` |

Existing suite remains the no-runtime-change check together with
`git diff --check` on this branch's production `.go` files (expected empty).
