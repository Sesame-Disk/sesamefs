# PC-0 — Multi-DC Publication Protocol Characterization

**Status:** characterization only. No `PublicationCoordinator` is implemented.
**Baseline:** rebased onto `main` at `f684171aa` (contains #206/#208/#209/#210).
Originally characterized against `a0eef9fb3` (contains #206/#208; did **not**
contain #209/#210); this file has been re-characterized below (§7, §8, §11,
§13) now that the rebase landed #210's real 3-DC `EACH_QUORUM` fallback for
Sync PutBlock cross-DC provenance
(`ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01`, resolved
2026-09-08). #209 (G2 PREPARED→COMMITTED handoff) touches only `internal/gc`
and is orthogonal to the publication funnels characterized here.
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
below. This characterization does not resolve whether ordinary GC
reachability already closes that gap independently of publish-time proof
quality; it is recorded as open, not answered. See §6 (PUBL-1/PUBL-2), §10,
and §15.

---

## 3. Inventory of HEAD publishers

Guards: `TestPC0AllHeadCallersAreInventoried` (lexical named HEAD calls in
the inventoried functions, not every callsite shape),
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
copy), `processSameRepoMove`, `RevertFile`, `RevertDirectory`,
`RestoreTrashItem`, `RevertDirents`, and the **source-library** HEAD in a
cross-repo move (`processSingleItem`).

R3 already excludes same-repo copy/move from the publication inventory. PC-0
agrees.

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

The HEAD primitive itself is `FSHelper.UpdateLibraryHead` (LWT).
`UpdateLibraryHeadFromSnapshot` is the v2/SeafHTTP/OnlyOffice entry.
Sync uses a **second** primitive, `updateLibraryHeadWithStats`.

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
        │  funnel-specific readiness      │
        │  and/or durable repair          │  order is funnel-specific;
        │  (FINAL exact-P where present)  │  exact-P is not universal.
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

The observed orders are frozen explicitly in the order table below and by
`TestPC0ObservedRepairReadinessPartialOrder`; the coordinator diagram in
§14 is a target boundary, not a description of every current funnel.

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
`TestPC0ObservedRepairReadinessPartialOrder` freezes the observed orders
below. Unifying them is a later PR with explicit evidence, not a PC-1 default.

```text
CreateFileFromBlocks / shared Once (when commitBlocks is populated):
  own up:<session>  →  claim session  →  stage pub  →  queue repair  →  exact-P  →  HEAD

Sync direct HEAD / auto-merge:
  stage pub  →  readiness (renew up + exact-P, provenanced only)  →  queue repair  →  HEAD

CreateFile / stored UploadFile / OnlyOffice / SeafHTTP / cross-repo:
  stage pub  →  queue repair  →  HEAD
  (no pre-HEAD readiness/exact-P; own liveness is whatever the prepare step left)
```

The common kernel is a **partial order**:

```text
stage pub
repair durable        \
readiness (if any)      > both before HEAD when present
                       /
HEAD → classify → settle
```

Readiness is optional **in today's code**. That optionality is a W2
publication-authority/continuity gap by provenance, not a second protocol.
Do not freeze readiness-before-repair as the coordinator spine: migrating
CreateFileFromBlocks behavior-preservingly requires keeping repair-before-fence
until an explicit unification PR.

For CFFB, capturing `ExpectedP` in the adapter (PUBLISHABLE?) is not the
final check: PC-2 must preserve `stage < repair < final exact-P revalidation
< HEAD`, matching F3's observed order (§5), unless a later PR explicitly
changes that order with new evidence. `PublishableInput` carrying `ExpectedP`
does not mean the exact-P check already ran.

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

## 6. Candidate invariants (PUBL-1 … PUBL-8)

| ID | Universal in today's code? | Notes |
|---|---|---|
| PUBL-1 Proven/publishable input | **No** | Classification exists in some adapters; Sync unprovenanced blocks and cross-repo borrowed `fs:` still enter `stage pub:`. `UNPROVENANCED` and `ERROR` are not publishable. `BORROWED` is not publishable until the adapter acquires durable own liveness; observing/revalidating foreign `fs:` is the W1 TOCTOU. The coordinator may accept only `PublishableInput`. Classifying those states inside the coordinator and then staging them would centralize the W2 hole (Sync without PutBlock still has no liveness attributable to the commit). `PublishableInput` as defined is scoped to dependencies newly live on the HEAD being published, not to dependencies inherited unchanged from the old HEAD (`ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01`). |
| PUBL-2 Publication authority / continuity | **No** | Exact-P before HEAD exists only for F3 placements and Sync-provenanced blocks. F2's fence is a no-op. That is a provenance-specific authority/continuity gap, not proof that every funnel must add a second exact-P read. Own `up:` + GC fence + install/repair can close materialization continuity via renewal/overlap; BorrowedFS/late pin still needs exact-P because the pin may arrive after GC won; cross-repo shows exact-P alone is TOCTOU without a dest own pin. |
| PUBL-3 No liveness gap | **Unproven (R31)** | Ordering aims at overlap; 48h TTL and `pub:` TTL still exist. |
| PUBL-4 Durable ambiguity | **Mostly** | UNKNOWN does not take known-loser cleanup. Repair row is the durable witness. Finite `pub:` TTL remains R31 (`ISSUE-GC-PUB-REF-ZERO-REF-01`). |
| PUBL-5 Known loser ≠ unknown | **Yes in request-local paths; no durable loser** | Classified differently; crash before cleanup collapses to UNKNOWN retain. |
| PUBL-6 Multi-DC absence | **Fixed for the scope-gate decision (#210, resolved)** | A `LOCAL_QUORUM` miss no longer settles the answer: `syncBlockHasOwnLivenessProvenanceFn` escalates to `BlockReferenceExistsEachQuorum` first, real 3-DC evidence attached. Only a **global** miss is treated as "no currently observable provenance" — still fail-open into the unprovenanced path (a separate, already-tracked W2 gap, not what #210 closed). Repair reachability fail-closes (retain). Destructive GC uses EACH_QUORUM (X2 closed) — different domain. |
| PUBL-7 Fail closed | **Yes for unavailable/error observations; by design open on a clean global miss** | Readiness failures abort before HEAD. A local scope-gate **error** fails closed without EQ. After a clean local miss, an `EACH_QUORUM` **error** also fails closed; an unavailable observation must not become absence. A clean global **miss** (successful read, no row) is treated as "no currently observable provenance" and takes the unprovenanced path. The #210 trade-off is specifically the clean **local** miss → EQ fallback needed to distinguish remote visibility from absence; #210 does not close or guarantee the global-miss W2 gap. Error and miss are distinct outcomes; do not collapse them. |
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
| Own-liveness evidence (Sync scope gate) | yes for **presence** (fast path) | **no** | **yes, on a clean local miss** (`BlockReferenceExistsEachQuorum`, #210, resolved 2026-09-08) | local miss escalates to `EACH_QUORUM`; a **global** miss ⇒ block left untouched, not fabricated (still not proof PutBlock never happened, see TTL expiry); global error fails closed exactly like a local error |
| CFFB/session `up:` write/renew | n/a (plain quorum upsert; an existing row renews and an expired row can be recreated) | n/a | no | upsert error aborts before HEAD |
| Exact-P validation | yes (advisory LQ) **given own pin already durable** | no (`Changed`/`Blocked`/error abort) | no | reject before HEAD; does not fabricate Authorized |
| `pub:` stage | n/a (write) | n/a | durability = session LQ write | stage failure ⇒ cleanup attempt, no HEAD |
| Repair intent | n/a (ordinary write) | n/a | must survive remote recovery | queue failure: Sync retains possible applied insert; v2 cleans attempt |
| HEAD CAS | Paxos domain (`SERIAL` default) | n/a | classify | UNKNOWN retained |
| v2 ambiguous CAS confirm | SERIAL read of HEAD | n/a | yes for APPLIED vs not | confirm error ⇒ `ErrLibraryHeadPublicationUnknown` |
| Sync CAS error | n/a | n/a | **no confirm** | always UNKNOWN (`errSyncHeadCASUncertain`) |
| Settlement promote `fs:` | session LQ writes | n/a | success is local quorum of the write | failure ⇒ schedule repair, do not unpublish HEAD |
| Repair reachability | no | **no** | SERIAL HEAD + EACH_QUORUM parents | non-reachable/unavailable ⇒ retain (`ISSUE-PUBLISH-REPAIR-REACHABILITY-01`) |
| Known-loser cleanup | request-local | n/a | must not run on UNKNOWN | crash ⇒ retain as UNKNOWN |

**Never:** `LOCAL_QUORUM miss` ⇒ globally absent. Since #210, the Sync scope
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
accept PublishableInput only
  (OWNED with own liveness, or BORROWED after acquiring durable own up:,
   capturing ExpectedP when required; observing foreign fs: without an
   own pin is not enough)
stage attempt-local pub:
durable repair intent and/or publication readiness
  (both before HEAD when present; relative order is not frozen here;
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
tracking R3's own `LogicalPositiveBlockDelta` caveat). Whether ordinary GC
reachability already makes that safe, independent of original publish-time
proof quality, is not established here and must be decided with evidence
before PC-2 migrates any funnel, not assumed by this boundary's silence.
PC-1 itself is skeleton/common types only, behavior-preserving, zero funnels
migrated; it does not need to (and must not) freeze full-work-set semantics
by implication, but PC-2 does pick a concrete `PublishableInput` shape and
cannot do that correctly until this question is answered.

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

| Funnel | DB shape | Local | Cross-DC / WAN | Paxos |
|---|---|---|---|---|
| CreateFile (empty) | O(1) tree + HEAD | tree reads | HEAD | 1 HEAD LWT |
| CreateFile (template) | + materialize | LQ pin/fence/install | HEAD; install LWT if new | HEAD + possible install |
| UploadFile stored | O(blocks) stage | LQ | HEAD | HEAD |
| CreateFileFromBlocks | O(blocks) renew+fence + session claim + optional successful slot release | LQ × N + quorum upserts | session-claim LWT + HEAD LWT under the configured serial domains; no added EQ on the normal path | session claim + HEAD; full successful requests also include the slot-release LWT when the cap is enabled |
| OnlyOffice | O(1) | LQ | HEAD | HEAD |
| SeafHTTP | O(blocks) stage + 1 commit INSERT | LQ | HEAD | HEAD |
| Cross-repo | O(copied files/blocks) | LQ | dest HEAD (+ source HEAD) | dest/source HEAD |
| Sync scope gate (N distinct candidates) | O(N) LQ scope reads | LQ × N | 1 EACH_QUORUM for each clean LQ miss; LQ errors abort without EQ | none beyond HEAD |
| Sync provenanced subset (P candidates) | remaining readiness/renewal for P after scope gate | LQ/EQ scope gate + funnel-specific readiness | repair cold SERIAL/EQ; HEAD | HEAD only |
| Sync unprovenanced subset (U global misses) | stage/repair/HEAD, no Sync readiness for those blocks | LQ/EQ scope gate still paid | no per-block readiness; HEAD | HEAD |

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
M1–M8. Rows marked prior evidence cite those scripts; they are not new
proofs from this PR. `UNKNOWN` / `GAP` / `PRIOR-EVIDENCE-NOT-RERUN` may
make the matrix complete. Reserve `executed evidence` / `leg executed` for
scenarios that actually ran a publication race.

Harness prints the recorded matrix rows. UNKNOWN is explicit, never silent
green.

| Row | Claim | Result on this baseline |
|---|---|---|
| M1 Local fast path | LQ presence / local tree / stage writes stay local | **OBSERVED** (source). Live 3-DC not required to see there is no EQ on those writes. |
| M2 Remote provenance before repair | PutBlock in dc-eu, HEAD in dc-na before hints | **PRIOR EVIDENCE** (`scripts/w2-sync-putblock-xdc-provenance-validation.sh`, #210 resolved 2026-09-08, real 3-DC RED→GREEN). Not re-executed by PC-0's own gate. A clean local miss now escalates to `EACH_QUORUM` before being treated as absence; a genuine global miss still skips W2 readiness for that block (separate, already-tracked gap, not what #210 closed). |
| M3 One DC down | EQ/SERIAL ops fail closed | Repair parent EQ and X2 are OBSERVED elsewhere. Sync's scope-gate `EACH_QUORUM` fallback specifically is now **PRIOR EVIDENCE** too (`TestW2SyncXDCFallbackFailsClosedWhenADatacenterIsDown3DC`, #210: fails closed, does not hang or report false absence). Publication HEAD (`SERIAL`) can still proceed if a quorum of the serial domain is available — **not re-measured here**. Funnel-complete M3 (every funnel, every EQ/SERIAL primitive) = still GAP. |
| M4 Cross-DC HEAD settlement | attempt in eu, repair in na | **PRIOR EVIDENCE — PARTIAL** (`scripts/w2-post-head-multidc-validation.sh`, CFFB/shared engine): cross-DC HEAD blindness does not authorize cleanup. Full remote replay/settlement, especially Sync-specific M4, remains **GAP** and was not re-executed by PC-0. |
| M5 Concurrent publishers | writer A na, writer B eu | CAS winner is Paxos-level **OBSERVED** (single-cluster tests). Live two-DC concurrent publishers = GAP. |
| M6 Cross-DC repair | pub/repair from one DC, worker in another | **EVIDENCE GAP** for the concrete DC-A write → DC-B discovery → settlement-worker proof; the W2 script's local-miss-not-cleanup observation is not that end-to-end proof, and PC-0 did not re-execute it. |
| M7 Stale placement | P changes before pre-HEAD fence | **MIXED**: F3 exact-P fence is **OBSERVED** (W1 retired-placement); Sync's provenanced subset has source/existing evidence for final exact-P validation; remaining funnels have no pre-HEAD exact-P fence = **GAP**. |
| M8 Funnel-specific | Sync, CFFB, stored v2, SeafHTTP, OO, cross-repo | **MIXED/PARTIAL**: CFFB/shared has classifier evidence, not full end-to-end multi-DC funnel proof; Sync xDC is **PRIOR EVIDENCE** (#210, see M2/M3); full 3-DC proof for OO/SeafHTTP/cross-repo remains **EVIDENCE GAP**. |

The table is a characterization matrix. Completeness means every row has a
status string, not that M1–M8 ran as publication races.

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
funnels. The stable observed kernel begins once a funnel stages:

```text
stage pub
→ funnel-specific repair and/or readiness
→ HEAD CAS → classify → settle
```

CFFB observes `verify/capture placement → own-liveness work → stage → repair →
final exact-P fence → HEAD`; Sync observes `stage → provenance/readiness →
repair → HEAD`, and can discover `UNPROVENANCED`/`ERROR` after staging. The
target coordinator contract below deliberately adds a stronger adapter
boundary to close that gap; it is not a claim about every current funnel.

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
          ├─ repair durable                                \
          ├─ publication readiness                           > both before
          │  (renew own liveness, and/or FINAL exact-P      /  HEAD when
          │  revalidation against ExpectedP -- after stage    present;
          │  and before HEAD; its relation to repair is       relative
          │  funnel-specific, never before staging)             order not
          │                                                     frozen here
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

Durable coordination remains Cassandra + appropriate CL/LWT domains.

### Relation to W2 / R31 / G4 / #209 / #210

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
- **#209/#210:** done. This branch is rebased onto a `main` that contains
  both (§ baseline note). Sync M2 / LQ-miss→EQ-fallback / the consistency
  map (§7, §8) / §11 / §13 have been re-characterized against #210's merged
  `BlockReferenceExistsEachQuorum` fallback and its real 3-DC evidence; the
  known issue it closed
  (`ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01`) is resolved, not
  reimplemented here. #209 (G2 PREPARED→COMMITTED handoff) touches only
  `internal/gc` and needed no re-characterization. Source contracts were
  rerun after the rebase (see below).

### Recommended next PR sequence (not frozen, not implemented)

```text
PC-0  this PR (characterization)
  → PC-1  coordinator skeleton / common types, behavior-preserving, zero funnels migrated;
          does not freeze full-work-set semantics
  → resolve ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01 with evidence, before PC-2
  → PC-2  migrate the best-understood funnel (CreateFileFromBlocks / shared Once)
          preserving today's stage < repair < final exact-P revalidation < HEAD
          order unless a separate PR unifies it with evidence
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
| `ISSUE-PC0-INHERITED-DEPENDENCY-CONTINUITY-01` | P1 | FOLLOW-UP / W2 (newly registered by PC-0, not introduced by it) | `PublishableInput`/the candidate coordinator boundary only cover dependencies a HEAD will *newly* live on. R3's own `LogicalPositiveBlockDelta` note says that delta is not the complete work set: dependencies inherited unchanged from the old HEAD, whose continuity was `CONDITIONAL`/`UNKNOWN` when first proven, are not covered. Not established whether ordinary GC reachability already closes this independently (§2, §6, §10). |
| `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` | P1 | FOLLOW-UP / W2 (newly registered by PC-0, not introduced by it) | Publication-authority/continuity before HEAD is not uniform by provenance. Exact-P exists only for CreateFileFromBlocks placements and Sync-provenanced blocks. `UploadFile` passes `nil` into the shared finalizer. CreateFile, OnlyOffice, SeafHTTP, cross-repo have no pre-HEAD fence. This does **not** prescribe exact-P as the only fix. |
| Sync PutBlock identity | P1 | already `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01`; cross-DC visibility slice closed by `ISSUE-SYNC-PUTBLOCK-CROSS-DC-PROVENANCE-VISIBILITY-01` (#210, resolved) | Evidence is still inference from `up:sync:<repo>:<block>` — #210 widened its visibility domain, not its identity (§11). The target coordinator must reject input with no provenanced PutBlock; today's Sync can still publish after a clean global miss, which remains the open W2 gap. |
| HEAD classify split | P2 | FOLLOW-UP / PC-1 | v2 confirms ambiguous CAS with SERIAL; Sync maps every CAS error to UNKNOWN without confirm. |
| Cross-repo own liveness | P1 | already R3 `UNKNOWN` | Destination does not take own `up:`. Exact-P alone would still be TOCTOU. |
| Known-loser durability | P2 | already `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01` | No durable loser witness. |
| Repair reachability | P1 | already `ISSUE-PUBLISH-REPAIR-REACHABILITY-01` | Unbounded ancestry / EQ availability. |
| `pub:` TTL | P1 | already R31 / `ISSUE-GC-PUB-REF-ZERO-REF-01` | Finite TTL can still open a liveness gap. |
| M3/M4/M5/M8 3-DC (remaining) | — | MATRIX GAP | M2/M3/M8's Sync-specific slice now has prior evidence (#210, §13). Still GAP: M3's funnel-complete claim (every funnel, every EQ/SERIAL primitive), M4's Sync-specific slice, M5 (live two-DC concurrent publishers), and M8's OO/SeafHTTP/cross-repo slice. |

W2, R31, and X1 remain OPEN.

---

## 16. Tests in this PR

| Test | Property |
|---|---|
| `TestPC0AllHeadCallersAreInventoried` | functions that lexically call named HEAD helpers must be classified; method values/aliases/second branches are out of scope |
| `TestPC0BlockPublicationFunnelsHaveMappedSeams` | each publication funnel has characteristic seams/stage/HEAD/settle symbols; characteristic seams are not assumed to be a universal pre-stage phase |
| `TestPC0R3StageToHeadInventoryIsSubset` | live R3 `r3PublicationStageToHeadBoundaries` labels are a subset of the PC-0 mapping |
| `TestPC0ObservedRepairReadinessPartialOrder` | CFFB `stage < repair < fence < HEAD`; Sync `stage < readiness < repair < HEAD`; auto-merge caller `stage < helper < HEAD` |
| `TestPC0CriticalConsistencyPrimitivesArePinned` | selected source tokens at named primitives; not the full consistency map; HEAD serial domain is not pinned |
| `TestPC0PublicationCoordinatorTypeIsNotImplemented` | no `PublicationCoordinator` type declaration (struct, interface, alias, or generic) anywhere under `internal/`, via an AST walk of every top-level `*ast.TypeSpec`, not a literal-string scan of three fixed directories |
| `TestPC0PublicationWrappersRemainAliases` | CreateFileFromBlocks/UploadFile/SeafHTTP wrappers still delegate |
| `TestPC0TreeMutationsDoNotCallBlockPublicationStageSeams` | tree-only HEAD callers do not invoke the known block-publication stage seams |
| `TestPC0StoredUploadExactPFenceIsNoOpWhenCommitBlocksNil` | UploadFile's nil `commitBlocks` path keeps the exact-P fence a no-op |
| integration `TestPC0PublicationMultiDCCharacterization` | 3-DC topology + matrix rows; gate cannot skip-green; GAP/UNKNOWN may complete the matrix |
| `scripts/pc0-publication-inventory-mutation-validation.sh` | 4/4 mutation legs RED: M1 untracked lexical publisher; M2 missing funnel seam; M3 tree mutation invoking a publication stage; M4 CL token downgrade |

Existing suite remains the no-runtime-change check together with
`git diff --check` on this branch's production `.go` files (expected empty).
