# PC-0 — Multi-DC Publication Protocol Characterization

**Status:** characterization only. No `PublicationCoordinator` is implemented.
**Baseline:** `main` at `a0eef9fb3` (contains #206 / #208; does **not** contain #209/#210).
**Scope:** documentation, source-contract tests, test-only 3-DC characterization harness.
**Not closed:** W2, R31, X1, G3/G4. `GC_ENABLED=false` remains required.
**Verdict:** `PROCEED WITH COORDINATOR` — see §14.

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
liveness (`pub:` / publish-attempt refs) and CAS library HEAD; reconstruct the
real `up → pub → HEAD → fs` protocol; classify consistency/visibility;
record crash/ambiguity/cleanup semantics; separate universal steps from
funnel-specific preparation; investigate whether Sync already has a durable
PutBlock→HEAD identity; freeze inventory/consistency with source contracts.

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
| **Adapter / evidence provider** | Funnel-specific preparation: bytes, canonical IDs, provenance, exact P, authorization to enter the common protocol. |
| **Own liveness** | A writer-owned `up:` referrer this process (or an equivalent retry) created. |
| **Exact P** | The currently observed canonical physical placement `(storage_class, storage_key)`. |
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

---

## 3. Inventory of HEAD publishers

Guards: `TestPC0AllHeadCallersAreInventoried`,
`TestPC0BlockPublicationFunnelsHaveMappedSeams`,
`TestR3PublicationStageToHeadHasNoUnlistedDirectDBCalls` (R3 baseline).

### 3.1 Block-publication funnels (enter `pub:` then HEAD)

| ID | Endpoint / wrapper | Prepare | Provenance | Stage | Readiness / exact P | Repair | HEAD helper | Settlement |
|---|---|---|---|---|---|---|---|---|
| F1 | `v2.CreateFile` | Office template PUT **or** empty file | Template: `RegisterUploadedBlockTargetAndMapping` with fresh `up:<uuid>`; empty file: no blocks | `stagePendingPublishedFiles` | **none** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule repair; known-loser request-local cleanup |
| F2 | `v2.UploadFile` → `finalizeStoredUploadMetadata` → `Once` | stored upload / SeafHTTP-compatible v2 | `RegisterUploadedBlockTarget` during upload (`up:<operation>`) | `stagePendingPublishedFiles` | `validateCommitBlockPublicationFences(commitBlocks)` — **no-op: UploadFile passes `nil`** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule; known-loser cleanup |
| F3 | `v2.CreateFileFromBlocks` → same `Once` | session blocks + BorrowedFS | `ensureCommitBlockOwnLiveness` (`up:<session>`) before claim | same `Once` | **yes**: `validateCommitBlockPublicationFences(commitBlocks)` after stage/repair, before HEAD | same | same | same; session claim is funnel-specific |
| F4 | `v2.processSingleItem` cross-repo copy/move | `copyFSObjectToLibraryForPublish` | source-repo `fs:` borrowed; **no destination own pin proven** | `stagePendingPublishedFiles` when `pendingCopiedFiles > 0` | **none** | `queuePendingPublishedFileRepairs` | dest `UpdateLibraryHeadFromSnapshot` | promote dest refs; source HEAD (move) is a separate tree mutation |
| F5 | `v2.publishEditedDocumentMetadata` (OnlyOffice) | callback download + `saveOnlyOfficePendingBlock` | callback `up:<operation>` | `stagePendingPublishedFiles` | **none** | `queuePendingPublishedFileRepairs` | `UpdateLibraryHeadFromSnapshot` | promote / schedule; pending-commit-id is OO-specific |
| F6 | `SeafHTTP.commitUploadedFileOnce` | single-block upload in this request | register during upload | `stageSeafHTTPPublishAttemptReferences` | **none** | `queuePublishedFSObjectBlockReferenceRepairFn` | `UpdateLibraryHeadFromSnapshot` | `finalizeSeafHTTPPublishedBlockReferences` |
| F7 | `SeafHTTP.commitUploadedFileMultiBlockOnce` | multi-block upload | register during upload | same | **none** | same | same | same |
| F8 | `Sync.handleSyncHeadPromotion` | client PutBlock / RecvFS / PutCommit | `up:sync:<repo>:<block>` **iff currently visible at LQ**; unprovenanced blocks skipped | `stageSyncCommitBlockDelta` **before** readiness | `ensureSyncCommitBlockPublicationReadiness` (provenanced subset only) | `queueSyncCommitBlockReferenceRepairsFn` **after** readiness | `updateLibraryHeadWithStats` | `finalizeSyncCommitBlockDeltaAndSettleRepairIntent`; shared repair never cleared on request-local loss |
| F9 | `Sync.tryAutoMergeSyncHeadPromotion` | merge commit of target onto current HEAD | same readiness | `stageSyncCommitBlockDelta` | same | queue after readiness; auto-merge commit ID includes a fresh UUID | `updateLibraryHeadWithStats` | structurally unique attempt: cleanup of `pub:` is safe; settlement still required |

`CreateFileFromBlocks` is **not** a distinct HEAD callsite. It is an adapter
in front of F2's finalizer, and the only current caller that populates
`commitBlocks` so F2's fence actually runs.

### 3.2 HEAD mutations that are not block publication

These CAS HEAD but do not stage new `pub:` block liveness. They are inventoried
so a future publisher cannot hide as "just another tree mutation":

`CreateDirectory`, `RenameDirectory`, `RenameFile`, `DeleteDirectory`,
`DeleteFile`, `BatchDeleteItems`, `copyItemWithinRepoWithRetry` (same-repo
copy), `processSameRepoMove`, `RevertFile`, `RevertDirectory`,
`RestoreTrashItem`, `RevertDirents`, and the **source-library** HEAD in a
cross-repo move (`processSingleItem`).

R3 already excludes same-repo copy/move from the publication inventory. PC-0
agrees.

### 3.3 Indirect wrappers

| Wrapper | Delegates to |
|---|---|
| `CreateFileFromBlocks` | `finalizeStoredUploadMetadata` |
| `finalizeStoredUploadMetadata` | `finalizeStoredUploadMetadataOnce` (retry on `ErrLibraryHeadConflict`) |
| `UploadFile` | `finalizeStoredUploadMetadata(..., nil)` |
| `commitUploadedFile` / `commitUploadedFileMultiBlock` | corresponding `Once` |

The HEAD primitive itself is `FSHelper.UpdateLibraryHead` (LWT).
`UpdateLibraryHeadFromSnapshot` is the v2/SeafHTTP/OnlyOffice entry.
Sync uses a **second** primitive, `updateLibraryHeadWithStats`.

---

## 4. Reconstructed current protocol

Observed productive control flow, not a new state machine table:

```text
FUNNEL PREPARE (bytes, tree, commit identity)
        ↓
BLOCKS_PROVEN?          // not universal; see §6
        ↓
PUB_STAGED              // attempt-local pub:  — DURABLE row, TTL-bound
        ↓
HEAD_AUTHORIZED?        // exact-P fence — NOT universal
        ↓
REPAIR_DURABLE          // published_block_reference_repairs — DURABLE
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

```text
CreateFileFromBlocks:
  own up:<session>  →  claim session  →  stage pub  →  queue repair  →  exact-P  →  HEAD

Sync direct HEAD / auto-merge:
  stage pub  →  readiness (renew up + exact-P, provenanced only)  →  queue repair  →  HEAD

CreateFile / stored UploadFile / OnlyOffice / SeafHTTP / cross-repo:
  stage pub  →  queue repair  →  HEAD
  (no exact-P; own liveness is whatever the prepare step left)
```

The common kernel is still:

```text
stage pub → (optional readiness) → durable repair → HEAD → classify → settle
```

Readiness is optional **in today's code**. It is a W2 completeness gap, not a
fundamentally different protocol.

---

## 5. Per-funnel matrix

W2 status uses R3 vocabulary: `PROVEN` / `CONDITIONAL` / `UNKNOWN`.

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
| W2 | `CONDITIONAL` (R3); exact-P **UNKNOWN/absent** |
| Common | stage, repair, HEAD, settle |
| Specific | upload materialize; nil placements |

Finding: `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01`.

### F3 — CreateFileFromBlocks

| Dimension | Observed |
|---|---|
| Content origin | Previously uploaded session blocks and/or foreign `fs:` |
| Operation identity | `session_id` (durable) + derived commit id |
| Block identity | Canonical SHA-256 before claim; SHA-1 for fs_object |
| Provenance | `SessionUpload` vs `BorrowedFS` vs `None` |
| Own liveness | `ensureCommitBlockOwnLiveness` upgrades/renews `up:<session>` |
| Exact P | `commitBlockPlacement` captured at verify; fenced immediately before HEAD |
| Fence | `ValidateBorrowedFSPublicationAuthority` @ `BlockAuthorityAdvisory` (LQ) |
| Repair | after stage, before fence/HEAD |
| Crash | session claim + repair row; session result recorded after success |
| Multi-process | session LWT claim is the adapter lock; repair is shared Cassandra |
| Multi-DC | pin is durable; fence is LQ (safe because own pin is already written) |
| Cost | O(distinct blocks) LQ reads+renew; O(files) repair; O(1) HEAD Paxos |
| W2 | `CONDITIONAL` through pre-HEAD (W1/W2 slices); R31 open |
| Common | proven blocks, stage, exact-P, repair, HEAD, settle |
| Specific | session claim, `/blocks/check`, BorrowedFS classify, digest idempotency |

This is the **clearest** current embodiment of the candidate coordinator kernel.

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
| Provenance | `BlockReferenceExistsLocalQuorum(up:sync:<repo>:<block>)` |
| Own liveness | renewed only if that LQ read is true; never fabricated |
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
| Multi-DC | LQ miss ⇒ skip readiness (**not** global absence). #210 not in this baseline |
| CL | provenance/fence LQ; HEAD LWT; repair cold path SERIAL + parent EACH_QUORUM |
| Paxos | HEAD CAS; not per-block |
| Cost | O(provenanced blocks) LQ; O(added files) repair; O(1) HEAD |
| W2 | `CONDITIONAL` for PutBlock-visible subset; `UNKNOWN` without PutBlock |
| Common | stage, readiness, repair, HEAD, classify, settle |
| Specific | commit parent/ancestry, auto-merge, RecvFS, CheckBlocks, stats/counters |

---

## 6. Candidate invariants (PUBL-1 … PUBL-8)

| ID | Universal in today's code? | Notes |
|---|---|---|
| PUBL-1 Proven input | **No** | Sync unprovenanced blocks and cross-repo borrowed `fs:` still enter publication. Coordinator should **require classified evidence**, including an explicit `UNPROVENANCED` class, rather than silently skipping. |
| PUBL-2 Exact P | **No** | Only F3 and Sync-provenanced blocks. F2's fence is a no-op. |
| PUBL-3 No liveness gap | **Unproven (R31)** | Ordering aims at overlap; 48h TTL and `pub:` TTL still exist. |
| PUBL-4 Durable ambiguity | **Mostly** | UNKNOWN does not take known-loser cleanup. Repair row is the durable witness. Finite `pub:` TTL remains R31 (`ISSUE-GC-PUB-REF-ZERO-REF-01`). |
| PUBL-5 Known loser ≠ unknown | **Yes in request-local paths; no durable loser** | Classified differently; crash before cleanup collapses to UNKNOWN retain. |
| PUBL-6 Multi-DC absence | **No for Sync provenance** | LQ miss is treated as "no provenance" (#210 open). Repair reachability fail-closes (retain). Destructive GC uses EACH_QUORUM (X2 closed) — different domain. |
| PUBL-7 Fail closed | **Mostly on repair/HEAD unknown** | Readiness failures abort before HEAD. Sync provenance miss is fail-**open** into the unprovenanced path. Unavailable global observation must not become absence — true for repair, not for Sync scope gate. |
| PUBL-8 Restart independence | **Partial** | APPLIED/UNKNOWN can be settled from another process via repair. Original process is not required. Known-loser cleanup is request-local. |

None of these refutes a coordinator. They show the coordinator must **own**
PUBL-1…8 rather than copy today's omissions.

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
| Own-liveness evidence (Sync scope gate) | yes for **presence** (fast path) | **no** | not implemented on this baseline (#210) | miss ⇒ skip readiness (fail open into unprovenanced) |
| CFFB/session `up:` renew | yes for presence | no (error fail-closed) | no | error aborts before HEAD |
| Exact-P validation | yes (advisory LQ) **given own pin already durable** | no (`Changed`/`Blocked`/error abort) | no | reject before HEAD; does not fabricate Authorized |
| `pub:` stage | n/a (write) | n/a | durability = session LQ write | stage failure ⇒ cleanup attempt, no HEAD |
| Repair intent | n/a (ordinary write) | n/a | must survive remote recovery | queue failure: Sync retains possible applied insert; v2 cleans attempt |
| HEAD CAS | Paxos domain (`SERIAL` default) | n/a | classify | UNKNOWN retained |
| v2 ambiguous CAS confirm | SERIAL read of HEAD | n/a | yes for APPLIED vs not | confirm error ⇒ `ErrLibraryHeadPublicationUnknown` |
| Sync CAS error | n/a | n/a | **no confirm** | always UNKNOWN (`errSyncHeadCASUncertain`) |
| Settlement promote `fs:` | session LQ writes | n/a | success is local quorum of the write | failure ⇒ schedule repair, do not unpublish HEAD |
| Repair reachability | no | **no** | SERIAL HEAD + EACH_QUORUM parents | non-reachable/unavailable ⇒ retain (`ISSUE-PUBLISH-REPAIR-REACHABILITY-01`) |
| Known-loser cleanup | request-local | n/a | must not run on UNKNOWN | crash ⇒ retain as UNKNOWN |

**Never:** `LOCAL_QUORUM miss` ⇒ globally absent. Today's Sync scope gate
violates the **spirit** of this rule by using miss to skip W2, not to
authorize GC. Distinct from X2.

---

## 8. Consistency map (OBSERVED vs REQUIRED)

Frozen by `TestPC0CriticalConsistencyPrimitivesArePinned`.

| Primitive | OBSERVED CL | REQUIRED for coordinator? |
|---|---|---|
| `BlockReferenceExistsLocalQuorum` | `LOCAL_QUORUM` | OBSERVED fast path. Global absence **must not** be inferred. #210 may add EQ fallback on **clean miss only** — not in this baseline. |
| `ValidateBorrowedFSPublicationAuthority` | `BlockAuthorityAdvisory` = LQ | REQUIRED: must stay off SERIAL on the publish hot path. |
| `ProbeBlockReuse` | session (prod LQ) | OBSERVED |
| `AddProvisionalBlockReferenceWithExpiry` | pinned producer (quorum write; see existing producer test) | OBSERVED renew; not Paxos |
| `AddPublishAttemptReferences` / stage | session LQ | OBSERVED |
| Repair INSERT/DELETE | ordinary session write, **no LWT** | OBSERVED; REQUIRED not to become per-block Paxos |
| `UpdateLibraryHead` CAS | `IF head_commit_id` + session `SerialConsistency` | REQUIRED: global serial domain for HEAD (do not design around `LOCAL_SERIAL`) |
| `confirmLibraryHeadCommitVisible` | `Consistency(SERIAL)` | OBSERVED v2 classify; candidate common classify step |
| Sync `updateLibraryHeadWithStats` CAS | same LWT, **no confirm** | OBSERVED split classifier |
| Repair HEAD read | SERIAL | OBSERVED cold path |
| Repair parent walk | EACH_QUORUM | OBSERVED cold path; availability cost is R31 |
| `BlockHasReferencesGlobal` | EACH_QUORUM | **not** on publication hot path (GC) |

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

### Repair worker (shared)

Positive reachability only promotes. Everything else retains. Timeout/lease
is **not** cleanup authority (closed `ISSUE-PUBLISH-REPAIR-TIMEOUT-CLEANUP-01`).
Parent walk EACH_QUORUM can be unavailable (`ISSUE-PUBLISH-REPAIR-REACHABILITY-01`).

---

## 10. Common vs funnel-specific

### Belongs in a future coordinator (universal kernel)

```text
accept ProvenPublicationInput (already classified)
stage attempt-local pub:
optional/required exact-P readiness against supplied placements
durable repair intent
HEAD attempt + classify APPLIED | KNOWN_LOSER | UNKNOWN
settlement:
  APPLIED → promote fs: / clear repair on success
  KNOWN_LOSER → exact attempt cleanup (and only then)
  UNKNOWN → retain
```

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
| Commit ID | only at PutCommit/HEAD, not at PutBlock | per commit, not per PutBlock | yes if committed | **no** | no |
| fs ID | at RecvFS | per file object | yes | not at PutBlock | no |
| repo ID | yes | no | yes | yes | no |
| `up:sync:<repo>:<block>` | yes | **no** — deterministic per (repo, block) | row survives; **LQ visibility is DC-local** | yes at PutBlock | **no** — same key for real PUT and later renew; absent after TTL or CheckBlocks skip |
| Upload token | auth only | no | session | n/a | no |
| Request identity | no durable | n/a | no | n/a | no |
| Server session | none for Sync blocks | n/a | n/a | n/a | no |
| Client protocol metadata | block id + bytes | content hash | n/a | n/a | CheckBlocks is the opposite of PutBlock |

**Finding (must not be softened):**

```text
current Sync evidence fundamentally requires inference
```

The only durable PutBlock residue is the deterministic `up:` referrer. It is
not a publication attempt id. Changing its name without changing the visibility
domain does not resolve #210.

#210 (LQ hit; EQ only on clean miss) is **not in this baseline**. After rebase
on a main that contains it, this section must be re-characterized. The
inventory of *identity* will not change: EQ fallback still infers from the
same key.

---

## 12. Cost / hot-path

No production cost is added by this PR.

| Funnel | DB shape | Local | Cross-DC / WAN | Paxos |
|---|---|---|---|---|
| CreateFile (empty) | O(1) tree + HEAD | tree reads | HEAD | 1 HEAD LWT |
| CreateFile (template) | + materialize | LQ pin/fence/install | HEAD; install LWT if new | HEAD + possible install |
| UploadFile stored | O(blocks) stage | LQ | HEAD | HEAD |
| CreateFileFromBlocks | O(blocks) renew+fence | LQ × N | HEAD only | HEAD (+ session claim LWT, adapter) |
| OnlyOffice | O(1) | LQ | HEAD | HEAD |
| SeafHTTP | O(blocks) stage + 1 commit INSERT | LQ | HEAD | HEAD |
| Cross-repo | O(copied files/blocks) | LQ | dest HEAD (+ source HEAD) | dest/source HEAD |
| Sync provenanced | O(N) LQ readiness | LQ × N | HEAD; repair cold SERIAL/EQ | HEAD only |
| Sync unprovenanced | scope-gate LQ miss per block | LQ | HEAD | HEAD |

Local fast path that **must** be preserved: LQ presence of own `up:` and LQ
exact-P **after** a durable own pin. Do not put EACH_QUORUM or SERIAL on that
path by default.

R3 declared exception for Sync readiness remains the only accepted O(N)
authority-shaped publish cost. PC-0 does not raise that budget.

---

## 13. 3-DC evidence

Reuse, do not duplicate:

- `scripts/x2-multidc-validation.sh` — GC EACH_QUORUM / topology
- `scripts/w2-post-head-multidc-validation.sh` — local blindness cannot cleanup
  a publication made in another DC
- Sync #210 harness — **not in this baseline**; record as GAP

Gate: `SESAMEFS_REQUIRE_PC0_PUBLICATION_CHARACTERIZATION=1`.
Skip under that gate is FAIL. Missing fixture is FAIL. Required named legs
must run. Default Docker `go-all-test` does **not** inherit this gate.

PC-0's own harness, when armed, connects to `dc-na`/`dc-eu`/`dc-asia` and
prints the matrix. It does **not** re-run `w2-post-head-multidc-validation.sh`
or `x2-multidc-validation.sh`. Rows marked prior evidence cite those scripts;
they are not new proofs from this PR.

| Leg | Claim | Result on this baseline |
|---|---|---|
| M1 Local fast path | LQ presence / local tree / stage writes stay local | **OBSERVED** (source). Live 3-DC not required to see there is no EQ on those writes. |
| M2 Remote provenance before repair | PutBlock in dc-eu, HEAD in dc-na before hints | **UNKNOWN / GAP** until #210. Current code: LQ miss ⇒ skip W2. |
| M3 One DC down | EQ/SERIAL ops fail closed | Repair parent EQ and X2 are OBSERVED elsewhere. Publication HEAD (`SERIAL`) can still proceed if a quorum of the serial domain is available — **not re-measured here**. Funnel-complete M3 = GAP. |
| M4 Cross-DC HEAD settlement | attempt in eu, repair in na | **PRIOR EVIDENCE** (`scripts/w2-post-head-multidc-validation.sh`, CFFB/shared engine). Not re-executed by PC-0. Sync-specific M4 = GAP. |
| M5 Concurrent publishers | writer A na, writer B eu | CAS winner is Paxos-level **OBSERVED** (single-cluster tests). Live two-DC concurrent publishers = GAP. |
| M6 Cross-DC repair | pub/repair from one DC, worker in another | **PRIOR EVIDENCE** (same W2 post-HEAD 3-DC script: local miss is not cleanup). Not re-executed by PC-0. |
| M7 Stale placement | P changes before pre-HEAD fence | **OBSERVED** for F3 (W1 retired-placement). Other funnels have no fence = GAP. |
| M8 Funnel-specific | Sync, CFFB, stored v2, SeafHTTP, OO, cross-repo | CFFB/shared: OBSERVED (W1/W2). Sync xDC: GAP (#210). OO/SeafHTTP/cross-repo 3-DC: **EVIDENCE GAP**. |

Harness prints the legs it executed. UNKNOWN is explicit, never silent green.

---

## 14. Architectural conclusion

### Verdict

```text
PROCEED WITH COORDINATOR
```

### Why the idea was not refuted

After inventorying every block-publication HEAD callsite, the same spine
appears:

```text
prepared/proven blocks → stage pub → readiness? → durable repair → HEAD → classify → settle
```

Differences are:

1. **Which adapter proves blocks** (session, PutBlock inference, OO download,
   copy).
2. **Which funnels omit readiness/exact-P** (completeness gap, not a second
   protocol).
3. **Two HEAD classifiers** (v2 SERIAL confirm vs Sync uncertain-on-any-error).
4. **Repair ownership** (unique attempt commit vs shared Sync `commit_id`).

Those are extraction and migration problems, not evidence that a common
protocol does not exist. Centralizing the spine would *reduce* W2 surface
(exact-P and UNKNOWN/loser rules today have to be re-proven per funnel).

### Why not "implement it in this PR"

The plan forbids it. Also: Sync identity is still inference; #210 is not
merged; exact-P is not universal; unifying HEAD classify is a behavior-sensitive
change that needs its own PR.

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
   ProvenPublicationInput
     - canonical blocks
     - classified evidence (own up / borrowed fs / unprovenanced / error)
     - exact P if the adapter claims physical dependence
     - attempt identity (commit/attempt id)
          │
          ▼
       COMMON  (stateless orchestration on every node)

 PublicationCoordinator
          │
          ├─ stage liveness
          ├─ exact-P readiness (required if input claims P)
          ├─ durable repair intent
          ├─ HEAD attempt + classify
          └─ settlement
```

Do not freeze Go APIs. The names above are documentation.

### Coordinator must be multi-DC from PC-1

Valid:

```text
attempt begins in dc-eu → process dies → repair continues in dc-na
→ dc-asia has a stale LQ view → system still converges
```

Invalid: home-DC coordinator, in-memory lock, "not visible locally ⇒ absent".

Durable coordination remains Cassandra + appropriate CL/LWT domains.

### Relation to W2 / R31 / G4 / #209 / #210

- **Coordinator ≠ W2 closed.** W2 still must prove: once D(P1) committed, no
  legitimate writer publishes durable liveness on P1.
- **Coordinator ≠ R31 closed.** Possibly-applied HEAD must not lose
  definitive liveness because `pub:` TTL expired.
- **G4** allows orphan COMMITTED(P1,D1) + blocks(L)=P2. Do not do G4 until
  the writer protocol (this spine) is uniform enough that every funnel obeys
  exact-P. This characterization is a prerequisite for that uniformity, not a
  G3 blocker.
- **#209/#210:** this branch started from current `main`. Rebase onto a main
  that contains them before merge and re-characterize Sync M2. Do not
  reimplement #210 here.

### Recommended next PR sequence (not frozen, not implemented)

```text
PC-0  this PR (characterization)
  → PC-1  coordinator skeleton / common types, behavior-preserving, zero funnels migrated
  → PC-2  migrate the best-understood funnel (CreateFileFromBlocks / shared Once)
  → PC-3… remaining v2/SeafHTTP/OnlyOffice/cross-repo
  → Sync adapter last
  → only then consider a Sync provenance redesign if identity remains inference
```

If PC-1 cannot unify HEAD classify without a behavior change, that change is a
**separate** explicit PR, not a silent extra in PC-1.

---

## 15. Findings (not fixed here)

| ID | Sev | Scope | Finding |
|---|---|---|---|
| `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` | P1 | W2 / CHARACTERIZATION-PR | Exact-P before HEAD exists only for CreateFileFromBlocks placements and Sync-provenanced blocks. `UploadFile` passes `nil` into the shared finalizer. CreateFile, OnlyOffice, SeafHTTP, cross-repo have no fence. |
| Sync PutBlock identity | P1 | already `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01` + `#210` issue | Evidence is inference from `up:sync:<repo>:<block>`. |
| HEAD classify split | P2 | FOLLOW-UP / PC-1 | v2 confirms ambiguous CAS with SERIAL; Sync maps every CAS error to UNKNOWN without confirm. |
| Cross-repo own liveness | P1 | already R3 `UNKNOWN` | Destination does not take own `up:`. |
| Known-loser durability | P2 | already `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01` | No durable loser witness. |
| Repair reachability | P1 | already `ISSUE-PUBLISH-REPAIR-REACHABILITY-01` | Unbounded ancestry / EQ availability. |
| `pub:` TTL | P1 | already R31 / `ISSUE-GC-PUB-REF-ZERO-REF-01` | Finite TTL can still open a liveness gap. |
| M2/M5/M8 3-DC | — | EVIDENCE GAP | Not claimed GREEN. |

W2, R31, and X1 remain OPEN.

---

## 16. Tests in this PR

| Test | Property |
|---|---|
| `TestPC0AllHeadCallersAreInventoried` | new productive HEAD callsite must be classified |
| `TestPC0BlockPublicationFunnelsHaveMappedSeams` | each publication funnel has prepare/stage/HEAD/settle symbols |
| `TestPC0R3StageToHeadInventoryIsSubset` | R3 list cannot drift off PC-0 |
| `TestPC0CriticalConsistencyPrimitivesArePinned` | OBSERVED CL tokens at named primitives |
| `TestPC0PublicationCoordinatorTypeIsNotImplemented` | no productive `PublicationCoordinator` type |
| `TestPC0PublicationWrappersRemainAliases` | CreateFileFromBlocks/UploadFile/SeafHTTP wrappers still delegate |
| integration `TestPC0PublicationMultiDCCharacterization` | named 3-DC legs; gate cannot skip-green |
| `scripts/pc0-publication-inventory-mutation-validation.sh` | M1 untracked publisher; M2 missing funnel seam; M3 CL downgrade |

Existing suite remains the no-runtime-change check together with
`git diff --check` on this branch's production `.go` files (expected empty).
