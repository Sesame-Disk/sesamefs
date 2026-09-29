# X1 critical path — reset to the #201 roadmap

**Status:** DECIDED 2026-09-29. Docs-only; no runtime, schema, CQL, guard or
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
identity). Between #216 and #233 (2026-09-12 → 09-26) no #201 step advanced:
the work went into the PublicationCoordinator skeleton, the PC-D1 certified
baseline line (witness, certifier, identity and mapping authority, lifecycle
fence decision) and the repair-liveness protocol attempts (#220/#222
abandoned, #223/#224).

Two causes, both process rather than architecture:

1. PC-D1 answered a W2 question — "who guarantees dependencies that were first
   published with `CONDITIONAL`/`UNKNOWN` continuity?" — with an after-the-fact
   certification architecture instead of finishing W2 at the source. On a
   greenfield server (historical backfill is a non-goal) a closed W2 leaves no
   unproven inherited dependency; the remaining risk is the GC Phase 5 defect,
   which is PRE-GC regardless.
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
- At most two audit rounds per PR. Later findings go to follow-ups unless they
  are a demonstrated defect under the first two rows of the table.
- "Demonstrate first": a §4 row can close with evidence (`CLOSED-EVIDENCE`)
  when the failure cannot be demonstrated; it does not need a new mechanism.

## 4. W2 exit checklist

Derived from the `CONDITIONAL`/`UNKNOWN` rows of
[R3-LIVENESS-CONTINUITY.md](./R3-LIVENESS-CONTINUITY.md) (the #201 definition
of W2) and the R31 residuals named in #201 §16. Row states: `OPEN`,
`CLOSED-EVIDENCE`, `CLOSED-FIX`. Criterion for every row (#201 §25 W2): once
D(P1) is committed, no legitimate writer may later publish durable liveness
that depends on P1.

**Reopen rule:** a newly discovered funnel reopens W2 only if it exists and is
reachable in `main` and violates the criterion. The possibility that someone
adds a funnel later does not.

| # | Row (R3 source) | Today | Tracking | State |
|---|---|---|---|---|
| W2-0 | **Cross-cutting:** continuous `up: → pub:` overlap under the 48h `up:` TTL (materialization primitive fresh/reuse rows: `up → fence clear → stall >48h → up expires → install` is not excluded; every `CONDITIONAL` row below repeats it) | unproven | R3 table; R31 | OPEN — investigate first whether one shared mechanism closes it for all funnels |
| W2-1 | `CreateFileFromBlocks`, exact session `up:` | pre-HEAD proven (#204/#205) | — | OPEN only through W2-0/R31 |
| W2-2 | `CreateFileFromBlocks`, foreign `fs:` reuse / dedup | W1 proven through HEAD (#202) | — | OPEN only through W2-0/R31 |
| W2-3 | Sync `PutBlock` → HEAD, and Sync retry from another pod | pre-HEAD proven for the PutBlock-provenanced subset, incl. cross-DC (#206/#210) | `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01` | OPEN |
| W2-4 | Sync commit whose block had no associated PutBlock | `UNKNOWN` | R3 table | OPEN |
| W2-5 | `recv-fs-before-put` | `UNKNOWN` | R3 table | OPEN |
| W2-6 | v2 stored upload (`UploadFile`), materialized and reusable target | `CONDITIONAL`; no exact-P | `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` | OPEN |
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

Suggested order: W2-0 first (it may close several rows at once); then W2-10
or W2-6, each starting with a demonstration of the failure; then the Sync
`UNKNOWN` rows, W2-9 and the R31 rows.

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

X1 CLOSED does not authorize `GC_ENABLED=true`. A1 additionally requires:

| Item | Tracking |
|---|---|
| Phase 5 expired-version cascade keeps fs_objects shared with HEAD (P0 latent) | `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` |
| Certification-window fence, **only as far as the GC destroyers D1–D3 need it** (re-scope PC-D1B.4 §18 before implementing; the full CW-M1..M34 list is not automatically required) | `ISSUE-PCD1B4-CERTIFICATION-WINDOW-FENCE-01` |
| HEAD-less ghost `libraries` row counts as canonically absent for GC/restore | `ISSUE-PCD1B-CONTINUITY-LWT-GHOST-ROW-01` |
| Repair-liveness residual (35-day `pub:` TTL): fail-closed GC health gate or equivalent decision | `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` (if not closed by W2-11) |
| GC mapping resolver `CassandraStore.lookupBlockMapping()` reclassified from session consistency | `PCD1B3-PRE-GC-SESSION-CONSISTENCY-EXCEPTION` marker |
| Library hard-delete lease: global SERIAL and fenced final batch | `ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01`, `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01` (current-runtime, §7) |
| Startup check refusing `GC_ENABLED=true` while any item above is open | first A1 item |

## 7. Current-runtime work outside the critical path

The library hard-delete lease issues are current-runtime defects, not only
PRE-GC: restore and `PermanentDeleteRepo` run through the API independently of
`GC_ENABLED`, and the cluster configurations use `LOCAL_SERIAL`. They are
worked in parallel with W2: first the global SERIAL pin of the lease LWTs
(`ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01`), then the generation-fenced
final batch (`ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`). The SERIAL pin alone
does not close the non-fencing race.
