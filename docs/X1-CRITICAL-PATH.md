# X1 critical path — reset to the #201 roadmap

## W2-0 follow-up — durable repair gate (2026-09-29)

Covered F1/F2/F3 and Sync-provenanced writers now acquire a non-expiring repair
before final exact-P. GC reads every organization repair page at EACH_QUORUM
and repeats the reference probe after a negative scan; acquisition pins LQ.
Sync preserves its captured placement through queueing. UNKNOWN/unreachable
rows retain existing R31 behavior, so stranded repairs can inhibit collection.
W2-0/W2-6a remain OPEN: network ambiguity, OS process-kill and rollout evidence
are pending. Older GC readers must be upgraded; cold-path cost is up to 32
organization range reads plus a ref probe, proportional to pending rows.
See [plan, evidence, individual re-audit and limits](./W2-0-PUBLICATION-CONTINUITY.md).

**Roadmap decision status (historical):** DECIDED 2026-09-29. Docs-only; no runtime, schema, CQL, guard or
mutation-suite change. **Base:** `main@a5dea859a` (PR #233 merged).
`GC_ENABLED=false` remains mandatory.

This document is the source of record for **the order of work toward X1
closure** and for **how audit findings are classified**. The X1 architecture
itself is unchanged and stays frozen in
[GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md](./GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md)
(D0, #201). Where another document states a different order (PC-D1B.5 →
productive consumer → PC-2 → …), this document supersedes that ordering; the
other documents keep their investigation and evidence.

## 1. Why this reset

Between #202 and #215 (2026-09-02 → 09-12) the project closed W1, the pre-HEAD
W2 slices for CreateFileFromBlocks/SessionUpload and Sync PutBlock, G1, G2, G3,
and several real runtime defects (H1, rollback ghost projections, Sync
identity). Between #216 and #233 (2026-09-12 → 09-26) most of the interval
was spent outside the #201 progression: apart from real R31 and runtime fixes
(#219 R31 convergence, #221 HEAD global SERIAL, #226 gone-check), the work went
into the PublicationCoordinator skeleton, the PC-D1 certified
baseline line (witness, certifier, identity and mapping authority, lifecycle
fence decision) and the repair-liveness protocol attempts (#220/#222
abandoned, #223/#224).

Two causes, both process rather than architecture:

1. PC-D1 answered a W2 question — "who guarantees dependencies that were first
   published with `CONDITIONAL`/`UNKNOWN` continuity?" — with an after-the-fact
   certification architecture instead of finishing W2 at the source. On a
   greenfield server (historical backfill is a non-goal) a closed W2 leaves no
   unproven inherited dependency; the remaining risk is GC destroying content
   HEAD still reaches (Phase 5 cascade, Phase 6 execute-time TOCTOU), which is
   PRE-GC regardless (§6).
2. Audit rounds turned hypothetical future bypasses into merge blockers, and
   decision records turned their own "must" lists into obligations for the next
   PR, creating a roadmap parallel to #201.

## 2. Critical path

```text
W1 ✅ → W2 (exit checklist §4) → G1 ✅ → G2 ✅ → G3 ✅ → G4 → G5 → E1 → X1 CLOSED
                                                                    │
                                              PRE-GC list (§6) → A1 → GC_ENABLED=true
```

- **G4** starts only when every W2 exit row (§4) is closed (#201 §25: "Only
  after W1+W2+G3").
- **E1** is the only step that may declare `X1 CLOSED` (#201 §25).
- **A1** is a separate PR; X1 CLOSED is not GC enabled.
- Every PR on the critical path names the #201 step (or the §4 row) it
  advances. Work that advances no step and fixes no current-runtime defect is
  not on the critical path.

## 3. Finding classification rule

| The failure sequence runs on… | Classification | Effect |
|---|---|---|
| current code, current runtime (`GC_ENABLED=false`) | CURRENT-RUNTIME bug | blocks the PR if the PR introduces it or breaks its contract; otherwise a separate follow-up |
| current code with the **planned** GC activation | X1 (if it is a §4/G4/G5/E1 item) or PRE-GC (§6) | valid finding; tracked in its list, blocks X1 or A1 respectively |
| code that does not exist (a new caller, alias, wrapper, CQL variant, future migration) | not a finding | recorded as a note at most; never a blocker |

Consequences:

- A finding must come with a concrete sequence over code that exists in the
  branch or in `main`.
- A mutation is valid when it removes a property that **current** correctness
  depends on. A mutation that invents a bypass absent from `main` to prove a
  guard would catch it is not required.
- Existing guards, inventories and mutation suites stay; no purge. New guards
  cover current call sites only.
- Decision records propose; they create obligations only through #201 steps,
  §4 or §6.
- Target: no more than two planned audit rounds per PR. The round number never
  changes a finding's severity or scope: a real finding introduced or worsened
  by the PR, or one that falsifies the PR's contract, blocks that PR whenever it
  is discovered; a real pre-existing independent finding is a follow-up
  whenever it is discovered.
- "Demonstrate first": do not assume a §4 row needs a new mechanism. A failure
  that could not be reproduced is **not** evidence of safety; closing a row
  needs one of the three closure states defined in §4.

## 4. W2 exit checklist

Derived from the `CONDITIONAL`/`UNKNOWN` rows of
[R3-LIVENESS-CONTINUITY.md](./R3-LIVENESS-CONTINUITY.md) (the #201 definition
of W2) and the R31 residuals named in #201 §16. Criterion for every row
(#201 §25 W2): once D(P1) is committed, no legitimate writer may later publish
durable liveness that depends on P1.

Row states:

| State | Meaning |
|---|---|
| `OPEN` | not closed |
| `CLOSED-EVIDENCE` | positive, reviewable evidence that the failure sequence is unreachable under the current code/runtime contract (for example a source-level argument that an existing bound already excludes it). "We tried and did not reproduce it" is not evidence |
| `CLOSED-FIX` | a change removes the failure sequence, with evidence |
| `CLOSED-GATED` | a named **fail-closed** mechanism (for example the GC health gate) refuses the dangerous transition — GC does not commit D(P1) — whenever the row's continuity premise may not hold, so the W2 violation is unreachable under the operational contract; E1 re-checks the mechanism |

A bound that only makes the violation rare, short-lived or operationally
tolerable does not close a row: the violation is still reachable, so the row
stays `OPEN`. Residual risk acceptance is not a W2 exit state.

**Reopen rule:** a newly discovered funnel reopens W2 only if it exists and is
reachable in `main` and violates the criterion. The possibility that someone
adds a funnel later does not.

| # | Row (R3 source) | Today | Tracking | State |
|---|---|---|---|---|
| W2-0 | **Cross-cutting:** continuous own liveness through HEAD, including the 48h `up:` and 35d `pub:` TTLs | Baseline TTL pins allowed expiry after exact-P and before HEAD. Current covered chains acquire a non-expiring repair before final exact-P; destructive absence observes that repair at EACH_QUORUM, with a final reference re-read covering promotion. A late acquisition must reject the settled deleting claim | R3 table; `ISSUE-W2-PUBLISH-PIN-EXPIRY-BEFORE-HEAD-01`; R31 | `OPEN` — durable repair gate plus final exact-P now protect the covered chains; network ambiguity, process-kill and mixed-deployment evidence remain required. See W2-0 follow-up above |
| W2-1 | `CreateFileFromBlocks`, exact session `up:` | pre-HEAD proven with the W2-0 mechanism (#204/#205) | — | OPEN through R31 and full W2-0 ambiguity/crash/rollout evidence |
| W2-2 | `CreateFileFromBlocks`, foreign `fs:` reuse / dedup | W1 proven through HEAD with the W2-0 mechanism (#202) | — | OPEN through R31 and full W2-0 ambiguity/crash/rollout evidence |
| W2-3 | Sync `PutBlock` → HEAD, and Sync retry from another pod | pre-HEAD proven for the PutBlock-provenanced subset, incl. cross-DC (#206/#210) | `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01` | OPEN |
| W2-4 | Sync commit whose block had no associated PutBlock | `UNKNOWN` | R3 table | OPEN |
| W2-5 | `recv-fs-before-put` | `UNKNOWN` | R3 table | OPEN |
| W2-6 | v2 stored upload (`UploadFile`), materialized and reusable target | Adopts the W2-0 mechanism: passes its materialized placement to the shared finalizer (PR #237). Real-Cassandra evidence `TestW2UploadFileExactPlacementBeforeHead` (GC-committed and fully-retired placement before stage → 409, HEAD unchanged, `pub:` dropped); RED on the previous code | `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` (UploadFile part) | `CLOSED-FIX` pre-HEAD; post-HEAD through W2-11…W2-14 |
| W2-6a | v2 `CreateFile`, Office-template block publication (empty-file path has no blocks) | Carries the actual materialized SHA-256/class/key to final exact-P after staging `pub:`. Nine real Cassandra/MinIO legs prove rejection when GC wins before validation, with 409, HEAD unchanged and cleanup. The follow-up adds real expiry-after-authority protection through a non-expiring repair gate; full ambiguity/crash/rollout evidence remains pending | `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` (CreateFile part; PC-0 F1); `ISSUE-W2-PUBLISH-PIN-EXPIRY-BEFORE-HEAD-01`; [plan/evidence](./W2-6A-CREATEFILE-EXACT-P.md) | `OPEN` — partial exact-P fix; covered expiry-after-validation is guarded; full ambiguity/crash/rollout and post-HEAD R31 remain OPEN |
| W2-7 | SeafHTTP normal/streaming finalize | `CONDITIONAL` | R3 table | OPEN |
| W2-8 | OnlyOffice callback | `CONDITIONAL` | R3 table | OPEN |
| W2-9 | Cross-repo copy/move | `UNKNOWN`; borrowed source `fs:` with no destination fence | R3 table | OPEN |
| W2-10 | Content resurrection: `RevertFile`, `RevertDirectory`, `RestoreTrashItem`, `RevertDirents` | `UNKNOWN`; no pin, `pub:`, repair or fence | `ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` | OPEN |
| W2-11 | R31: repair after HEAD — renewal after classify | open | `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` | OPEN |
| W2-12 | R31: known-loser durability | open | `ISSUE-PUBLISH-REPAIR-KNOWN-LOSER-DURABILITY-01` | OPEN |
| W2-13 | R31: repair discovery bound | open | `ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01` | OPEN |
| W2-14 | R31: `pub:` zero-ref transition | open | `ISSUE-GC-PUB-REF-ZERO-REF-01` | OPEN |

Not W2 exit rows (follow-ups, may be reclassified in E1):
`ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01`,
`ISSUE-PUBLISH-REPAIR-DEAD-ROW-RETENTION-01`,
`ISSUE-PUBLISH-REPAIR-CROSS-CHUNK-CYCLE-01`,
`ISSUE-PUBLISH-REPAIR-PROGRESS-PAXOS-DOMAIN-01`,
`ISSUE-SYNC-PUTBLOCK-READINESS-HOTPATH-COST-01`.

Suggested order: resolve W2-0 pin-expiry continuity before claiming full closure of an adopting funnel. W2-6a remains OPEN. Existing W2-1/2/6 closure language is historical and must be re-audited against this shared premise, not treated as proof of unbounded continuity. Other scoped work:
W2-7/W2-8 or W2-10, each starting with a
demonstration of the failure; then the Sync `UNKNOWN` rows, W2-9 and the R31
rows.

## 5. PublicationCoordinator and the PC-D1 line

- `internal/publication` (#216) and the PC-D1 line (#227, #228, #230, #231,
  #233) stay in `main` unchanged. They are **frozen**: no new work unless a §4
  row or a §6 item needs it.
- The coordinator is an optional tool for W2: migrating a funnel to it is
  acceptable when that is the minimal way to close a §4 row. PC-2 is a future
  evolution of the publication architecture, **not** an X1 prerequisite.
- A productive consumer of the certified witness is not on the critical path.
  When one is built, it owns CW-M17 (witness consumed only under global SERIAL
  or inside an LWT of the same domain).
- Identity authority (#230/#231) keeps its current-runtime value: a writer
  cannot replace a claimed commit/fs_object digest.
- E1 re-evaluates whether the certified baseline is needed at all once W2 is
  closed.
- PC-D1B.5 work in progress is parked on
  `feat/pc-d1b5-certification-window-fence-runtime` (`8a731e6bc`: migration
  `028`, E/P/S capture/intent/completion primitives, certifier capture and
  epoch predicate, `DestructionTokenV1`). It is reference material, not
  scheduled work.

## 6. PRE-GC list (A1 prerequisites)

X1 CLOSED does not authorize `GC_ENABLED=true`. Documents written before this
reset (operational notes, config comments, dated readiness reports, analysis
docs) that say GC may be activated "after X1 closes" mean after this gate
closes. **The A1 gate is every open
entry tagged `PRE-GC` or `PRE-ACTIVATION` in
[KNOWN_ISSUES.md](./KNOWN_ISSUES.md) and
[TECHNICAL-DEBT.md](./TECHNICAL-DEBT.md), plus the table below.** The table
names the known items; it is not a substitute for the tags. Enumerate the tags
before A1, for example:

```sh
grep -nE 'PRE-GC|PRE-ACTIVATION' docs/KNOWN_ISSUES.md docs/TECHNICAL-DEBT.md docs/OPEN-WORK-INDEX.md
```

A new PRE-GC finding is registered with that tag; it does not need to be
added here to count.

| Item | Tracking |
|---|---|
| Phase 5 expired-version cascade keeps fs_objects shared with HEAD (P0 latent) | `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` |
| Phase 6 execute-time TOCTOU: the keep-set is computed at scan time and fs_object items carry no library guard and no execute-time reachability recheck, so a HEAD that re-references an fs_id between scan and execution loses the fs_object row. Closing W2-10 (block liveness for content resurrection) does **not** close this; the certification-window fence does not either (PC-D1B.4 §15) | recorded under `ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` (PC-D1B.4 finding F3) |
| Stale-claim settle race: after `ReleaseStaleBlockClaim` reports `BlockClaimAbsent`, `settleBlockCandidate` deletes the candidate conditioned only on `candidate_at`, so a claim won by another worker in the gap can lose its recovery authority (liveness: block stranded in `deleting`, no data loss). Close here or make it an explicit G5 exit criterion | `ISSUE-GC-STALE-CLAIM-SETTLE-RACE-01` |
| Certification-window fence, **only as far as the GC destroyers D1–D3 need it** (re-scope PC-D1B.4 §18 before implementing; the full CW-M1..M34 list is not automatically required). Includes `ISSUE-PCD1B-STALE-TOMBSTONE-DISPLAY-METADATA-01` if generation-timestamped deletes are adopted | `ISSUE-PCD1B4-CERTIFICATION-WINDOW-FENCE-01` |
| HEAD-less ghost `libraries` row counts as canonically absent for GC/restore | `ISSUE-PCD1B-CONTINUITY-LWT-GHOST-ROW-01` |
| Repair-liveness residual (35-day `pub:` TTL): the fail-closed GC health gate, if W2-11 is closed as `CLOSED-GATED` through it, must exist and be enabled before activation | `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` (W2-11) |
| GC mapping resolver `CassandraStore.lookupBlockMapping()` reclassified from session consistency | `PCD1B3-PRE-GC-SESSION-CONSISTENCY-EXCEPTION` marker |
| Library hard-delete lease: global SERIAL and fenced final batch | `ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01`, `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01` (current-runtime, §7) |
| Startup check refusing `GC_ENABLED=true` while any A1 gate item (tagged entry or row above) is open | first A1 item |

Closed by the greenfield precondition, not carried into A1: Technical Debt
#24 (pre-migration-017 `gc_block_candidates` rows with a null `storage_key`).
The deployment that will enable GC starts from an empty server (no legacy data
is preserved), so it cannot contain candidate rows written before migration
`017`. If GC is ever enabled on a deployment that ran pre-`017` code, #24
reopens as PRE-ACTIVATION.

## 7. Current-runtime work outside the critical path

The library hard-delete lease issues are current-runtime defects, not only
PRE-GC: restore and `PermanentDeleteRepo` run through the API independently of
`GC_ENABLED`. The SERIAL-domain issue is **config-dependent**: the shipped
`configs/config.prod.yaml` uses `serial_consistency: SERIAL`, but
`LOCAL_SERIAL` is a supported setting (used by the multi-region compose
configurations; startup only logs a warning) and the lease LWTs inherit it.
Both issues are worked in parallel with W2: first the global SERIAL pin of the lease LWTs
(`ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01`), then the generation-fenced
final batch (`ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`). The SERIAL pin alone
does not close the non-fencing race.
