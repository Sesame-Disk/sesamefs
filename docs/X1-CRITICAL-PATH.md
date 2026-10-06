# X1 critical path — reset to the #201 roadmap

## G5 covered development contract validated (2026-10-02)

G4 is merged in #247, main@b57f5ce69. The [G5 implementation plan](./GC-G5-IMPLEMENTATION-PLAN.md)
replaces the physical day walk with bounded durable-root execution and seek
checkpoints. The same-P stale-claim settlement race reproduced before PREPARED;
its attempted candidate-retention fix is withdrawn after a reproduced fresh
zero-epoch grace regression. The race remains OPEN, P1 FOLLOW-UP / PRE-GC.
The earlier single-DC full integration passed; expanded three-DC integration
failed 30 main tests and API failed 8/20 suites at 1a1cb9629. These failures
remain recorded for E1; neither combined nor full three-DC evidence is a PASS.
E1 remains next. No W2/R31, X1, PRE-GC or activation gate is closed by this entry.
GC stays OFF.

## G4/G5 development decision (2026-10-02)

The [G4 implementation plan](./GC-G4-IMPLEMENTATION-PLAN.md) supersedes the
old all-W2-closed prerequisite for developing G4/G5. W2/R31 rows remain
recorded with their existing dispositions; evaluate residuals against the
completed GC in E1 and PRE-GC, and fix demonstrated activation blockers.
Open residuals alone do not block this development. GC remains disabled.
Dated evidence below preserves its historical conclusions.

## Greenfield first-production reconciliation (2026-09-30)

W2-0 is **CLOSED-EVIDENCE for the covered current-version mechanism** under
the [first-production contract](./X1-CRITICAL-PATH.md#first-production-deployment-contract).
The #239 durable repair / final exact-P ordering and #241 native wire loss,
real SIGKILL, independent recovery and three-DC evidence are preserved.
Both incompatible mixed-version combinations still reproduce D(P)+HEAD;
ISSUE-W2-INCOMPATIBLE-MIXED-ROLLOUT-01 tracks them as P1 FOLLOW-UP / GENERAL,
outside v1 first activation. No compatibility gate is inferred or added.
W2-1, W2-2 and W2-6a remain OPEN through the separately tracked R31 residuals.
W2-4 now has a focused pre-HEAD fix with RED/GREEN evidence; its post-HEAD
R31 requirements remain separate. Other W2 dispositions are unchanged.
The G4/G5 development decision above supersedes the old all-W2 prerequisite; GC stays disabled.

See the [source argument, individual re-audit and drift review](./W2-0-GREENFIELD-RECONCILIATION.md).

The dated #238/#239 entries and characterization tables below are historical
evidence snapshots. Their old W2-0 OPEN / pending-evidence statements are
superseded by this reconciliation and the current X1 checklist; they do not
define today's closure state.

### PR #239 crossed audit correction (2026-09-30)

Pre-D GC now distinguishes real references, repair-only protection and zero.
Repair-only releases the exact claim and preserves candidate/discovery/queue,
postponing without retry. After COMMITTED, only actual references retain the
existing contradiction policy; a late repair cannot veto D. Both worker
regressions are included in 17 mandatory continuity legs. G2/G3 retirement and
durable physical continuation are proved; the future physical executor remains
outside this PR. W2-0/W2-6a remain OPEN and GC_ENABLED=false.
See [crossed audit evidence](./W2-0-PUBLICATION-CONTINUITY.md).

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
W1 + W2 core evidence + G1/G2/G3
  -> G4 -> G5 -> E1 (re-evaluate W2/R31) -> X1 CLOSED
  -> PRE-GC targeted validation -> A1 -> GC_ENABLED=true
```

- **G4/G5 development** proceeds with GC OFF after W1, covered W2 core
  evidence and G1/G2/G3. Remaining W2/R31 rows are evaluated in E1 against
  completed GC; unresolved activation requirements still block A1.
- **E1** is the only step that may declare `X1 CLOSED` (#201 §25).
- **A1** is a separate PR; X1 CLOSED is not GC enabled.
- Every PR on the critical path names the #201 step (or the §4 row) it
  advances. Work that advances no step and fixes no current-runtime defect is
  not on the critical path.

### First-production deployment contract

The supported v1 deployment is **greenfield**: a clean server, new Cassandra
keyspace and storage namespaces, with no inherited production dataset,
historical candidates/repairs/attempts, old processes or old in-flight requests.
Before first production traffic, all productive writers, repair workers,
scanners and GC workers run the same compatible audited release. All migrations
are applied before traffic. GC stays disabled fleet-wide during deployment,
startup, smoke tests, consistency/cluster validation and until PRE-GC and A1
close. This is a deployment obligation, not an implemented version registry
or a claim that GC disablement proves writer continuity.

This reconciles the existing [deployment scope](./DEPLOY.md#first-production-greenfield-contract),
[metadata-identity authority contract](./PC-D1B-METADATA-IDENTITY-AUTHORITY.md)
and the empty-server precondition already used for Technical Debt #24 in §6.
There is no production v1 legacy fleet to upgrade. A later incompatible rolling
upgrade with active GC is outside this first-production contract; its real
counterexamples remain recorded as P1 FOLLOW-UP / GENERAL. If that deployment
model is adopted, compatibility/drain evidence must be supplied before claiming
it safe. Greenfield does not remove current-version races, wire ambiguity,
process death, cross-DC visibility, R31 or any PRE-GC/A1 prerequisite.

## 3. Finding classification rule

| The failure sequence runs on… | Classification | Effect |
|---|---|---|
| current code, current runtime (`GC_ENABLED=false`) | CURRENT-RUNTIME bug | blocks the PR if the PR introduces it or breaks its contract; otherwise a separate follow-up |
| current code with the **planned** GC activation | X1 (if it is a §4/G4/G5/E1 item) or PRE-GC (§6) | valid finding; tracked in its list, blocks X1 or A1 respectively |
| code that exists but requires an unsupported incompatible deployment (for example pre-#239 writer/GC skew) | real FOLLOW-UP / GENERAL finding; severity preserved | not a v1 X1/PRE-GC/A1 blocker; preserve counterexample and deployment limit |
| code that does not exist (a new caller, alias, wrapper, CQL variant, future migration) | not a finding | recorded as a note at most; never a blocker |

Consequences:

A strong test of an unsupported incompatible deployment remains valid evidence
of that limitation. State the deployment preconditions: test strength alone
does not make the sequence reachable under the supported v1 contract or turn
it into a v1 blocker.

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
| W2-0 | **Cross-cutting:** continuous own liveness through HEAD, including the 48h `up:` and 35d `pub:` TTLs | Baseline TTL pins allowed expiry after exact-P and before HEAD. Current covered chains acquire a non-expiring repair before final exact-P; destructive absence observes that repair at EACH_QUORUM, with a final reference re-read covering promotion. A late acquisition must reject the settled deleting claim | R3 table; `ISSUE-W2-PUBLISH-PIN-EXPIRY-BEFORE-HEAD-01`; R31 | **CLOSED-EVIDENCE** for the covered current-version mechanism under the first-production contract: durable repair before final exact-P, destructive EACH_QUORUM observation, final reference re-read, native wire/SIGKILL/recovery and 3-DC evidence. No independent current-version W2-0 residual identified; R31 settlement/liveness and unadopted funnels remain their own OPEN rows |
| W2-1 | `CreateFileFromBlocks`, exact session `up:` | pre-HEAD proven with the W2-0 mechanism (#204/#205) | — | OPEN through R31 (W2-11..14): renewal-after-classify, known-loser durability, discovery bound and zero-ref transition; covered pre-HEAD continuity re-audited against closed W2-0 and real wire/SIGKILL evidence |
| W2-2 | `CreateFileFromBlocks`, foreign `fs:` reuse / dedup | W1 proven through HEAD with the W2-0 mechanism (#202) | — | OPEN through R31 (W2-11..14): renewal-after-classify, known-loser durability, discovery bound and zero-ref transition; covered pre-HEAD continuity re-audited against closed W2-0 and real wire/SIGKILL evidence |
| W2-3 | Sync `PutBlock` → HEAD, and Sync retry from another pod | pre-HEAD proven for the PutBlock-provenanced subset, incl. cross-DC (#206/#210) | `ISSUE-SYNC-PUTBLOCK-EXPIRED-PROVENANCE-01` | OPEN |
| W2-4 | Sync commit whose block had no associated PutBlock | Every added canonical placement is captured and revalidated after durable repair; no Sync upload pin is fabricated. Ten real Cassandra/MinIO safety legs cover direct/auto-merge, GC-first rejection, fully retired P, successful controls, and repair ownership; RED on PR #244. See [investigation/fix](./W2-4-SYNC-NO-PUTBLOCK.md) | `ISSUE-W2-SYNC-NOPUTBLOCK-PREHEAD-01` | **CLOSED-FIX pre-HEAD**; post-HEAD W2-11..14 remain OPEN |
| W2-5 | `recv-fs-before-put` | Ordered direct fresh/reuse/retirement subset CLOSED-EVIDENCE (E1-7); broader protocol/post-HEAD UNKNOWN | [E1-7 evidence](./E1-7-SYNC-RECVFS-BEFORE-PUTBLOCK.md) / R3 table | OPEN overall; measured contract only |
| W2-6 | v2 stored upload (`UploadFile`), materialized and reusable target | Adopts the W2-0 mechanism: passes its materialized placement to the shared finalizer (PR #237). Real-Cassandra evidence `TestW2UploadFileExactPlacementBeforeHead` (GC-committed and fully-retired placement before stage → 409, HEAD unchanged, `pub:` dropped); RED on the previous code | `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` (UploadFile part) | `CLOSED-FIX` pre-HEAD; post-HEAD through W2-11…W2-14 |
| W2-6a | v2 `CreateFile`, Office-template block publication (empty-file path has no blocks) | Carries the actual materialized SHA-256/class/key to final exact-P after staging `pub:`. Nine real Cassandra/MinIO legs prove rejection when GC wins before validation, with 409, HEAD unchanged and cleanup. The follow-up adds real expiry-after-authority protection through a non-expiring repair gate; real wire/SIGKILL evidence now covers the current-version chain under the first-production contract | `ISSUE-PC0-EXACT-P-FUNNEL-GAP-01` (CreateFile part; PC-0 F1); `ISSUE-W2-PUBLISH-PIN-EXPIRY-BEFORE-HEAD-01`; [plan/evidence](./W2-6A-CREATEFILE-EXACT-P.md) | **OPEN** — exact-P and covered pre-HEAD continuity proved; post-HEAD R31 (W2-11..14: renewal, known-loser durability, discovery and zero-ref transition) remains OPEN |
| W2-7 | SeafHTTP normal/streaming finalize | E1-5a single-shot and E1-5b streaming retain original materialized P through retries, validate after durable repair before HEAD, reject COMMITTED/TERMINAL; streaming same-tracker retry rematerializes only rejected blocks | [E1-5b evidence](./E1-5B-SEAFHTTP-STREAMING-PUBLICATION-SAFETY.md) / [E1-5a evidence](./E1-5A-SEAFHTTP-SINGLE-PUBLICATION-SAFETY.md) | **CLOSED-FIX measured single-shot and streaming pre-HEAD**; post-HEAD/R31 OPEN |
| W2-8 | OnlyOffice callback | E1-4 retains original materialized exact P through durable repair and validates before HEAD; current-code COMMITTED/TERMINAL callbacks were RED | [E1-4 evidence](./E1-4-ONLYOFFICE-PUBLICATION-SAFETY.md) | **CLOSED-FIX for measured pre-HEAD contract**; post-HEAD cleanup/recovery and R31 remain OPEN |
| W2-9 | Cross-repo copy/move | E1-09 captures source exact P per retry and checks it after durable repair before HEAD; productive source-purge COMMITTED/TERMINAL RED -> GREEN | [E1-09](./E1-09-CROSS-REPO-PUBLICATION-SAFETY.md) | CLOSED-FIX measured single-file/block plaintext same-org/representation pre-HEAD; broader W2-9/post-HEAD R31 OPEN |
| W2-10 | Content resurrection: `RevertFile`, `RevertDirectory`, `RestoreTrashItem`, `RevertDirents` | No own pin, `pub:`, repair or fence. E1-6 RevertFile retained-history measured; #259 single-file restore and E1-10b one-directory/one-child restore cover narrow retained-history schedules; E1-10c adds one-directory/one-child RevertDirectory; E1-10d measures single-root-file/path RevertDirents. Historical child fs: blocks measured GC before claim/D; COMMITTED/TERMINAL unexecuted | [E1-6](./E1-6-REVERTFILE-PUBLICATION-SAFETY.md), [#259 file evidence](./E1-10-RESTORETRASH-RETAINED-HISTORY.md), [E1-10b directory evidence](./E1-10B-RESTORETRASH-DIRECTORY-HISTORY.md), [E1-10c directory revert](./E1-10C-REVERTDIRECTORY-RETAINED-HISTORY.md), [E1-10d dirent revert](./E1-10D-REVERTDIRENTS-RETAINED-HISTORY.md) / `ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` | OPEN; no CLOSED-FIX or full pre-HEAD safety; broader RevertDirents batch/shapes/policies and retention/Phase5/6 remain open |
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

E1 evidence update (2026-10-03): with Cassandra plus pinned SILO
`RELEASE.2026-09-16T00-00-00Z` (MinIO-compatible S3), a real delayed Sync PutBlock after terminal D
left bytes at the retired K1 but rematerialized the canonical block at P2 and
did not restore a P1 reference. A post-terminal replay of a repair for a
commit that lost HEAD retained UNKNOWN and `pub:` only. The reachable-HEAD
late-repair race and other current funnels remain untested; these results do
not close W2-3, W2-11..14, or X1. See
[E1 execution ledger](./E1-FULL-GC-PUBLICATION-RECOVERY.md).

E1-2 adds [reachable-HEAD gate evidence](./E1-2-REACHABLE-HEAD-LATE-REPAIR.md):
after real temporary-reference TTL expiry, the Office writer's durable repair
vetoes a new D before handoff, then recovers to exact fs: using a fresh session.
This narrows the covered gate's evidence; it does not close E1-02, W2-11/14 or
X1 and does not claim COMMITTED/TERMINAL post-D execution.

E1-3 adds [two pre-D ordering controls](./E1-3-LATE-PUBLICATION-PRE-D-PROOF.md)
for Office/shared repair: final exact-P rejects a late writer under the held
claim; reachable settlement before a negative repair scan is caught by the
second global refs read. Evidence is narrow; W2-11/14 and X1 remain OPEN.

Historical suggested W2 order (superseded for G4/G5 development by the
2026-10-02 decision): W2-0 is closed for the covered shared mechanism. W2-1/2/6a were individually re-audited against that premise and remain OPEN through R31, not mixed rollout. W2-6 retains CLOSED-FIX pre-HEAD. Finish the real remaining funnels:
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
| G5 cycle revisit assumes advancing claim clocks with bounded, observed fleet skew and finite pre-cutoff claims. NTP/equivalent plus a verified clock-health activation/suspension procedure is required PRE-GC; G5 provides no enforcement and no fixed wall-clock revisit bound | `ISSUE-GC-ROOT-CYCLE-CLOCK-ASSUMPTION-01` |
| Phase 5 expired-version cascade keeps fs_objects shared with HEAD (P0 latent) | `ISSUE-GC-PHASE5-CASCADE-SHARED-FSOBJECTS-01` |
| Phase 6 execute-time TOCTOU: the keep-set is computed at scan time and fs_object items carry no library guard and no execute-time reachability recheck, so a HEAD that re-references an fs_id between scan and execution loses the fs_object row. Closing W2-10 (block liveness for content resurrection) does **not** close this; the certification-window fence does not either (PC-D1B.4 §15) | recorded under `ISSUE-PC0-CONTENT-RESURRECTION-PUBLICATION-01` (PC-D1B.4 finding F3) |
| Stale-claim settle race: after `ReleaseStaleBlockClaim` reports `BlockClaimAbsent`, `settleBlockCandidate` deletes the candidate conditioned only on `candidate_at`, so a claim won by another worker in the gap can lose its recovery authority (liveness: block stranded in `deleting`, no data loss). OPEN, P1 FOLLOW-UP / PRE-GC; evaluate in E1. Outside G5 durable-root scope; closure must preserve scheduling independently of fresh zero-ref grace | `ISSUE-GC-STALE-CLAIM-SETTLE-RACE-01` |
| Certification-window fence, **only as far as the GC destroyers D1–D3 need it** (re-scope PC-D1B.4 §18 before implementing; the full CW-M1..M34 list is not automatically required). Includes `ISSUE-PCD1B-STALE-TOMBSTONE-DISPLAY-METADATA-01` if generation-timestamped deletes are adopted | `ISSUE-PCD1B4-CERTIFICATION-WINDOW-FENCE-01` |
| HEAD-less ghost `libraries` row counts as canonically absent for GC/restore | `ISSUE-PCD1B-CONTINUITY-LWT-GHOST-ROW-01` |
| Repair-liveness residual (35-day `pub:` TTL): the fail-closed GC health gate, if W2-11 is closed as `CLOSED-GATED` through it, must exist and be enabled before activation | `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` (W2-11) |
| GC mapping resolver `CassandraStore.lookupBlockMapping()` reclassified from session consistency | `PCD1B3-PRE-GC-SESSION-CONSISTENCY-EXCEPTION` marker |
| Library hard-delete lease: global SERIAL (✅ closed by PR #234) and fenced final batch | `ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01` (closed), `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01` (open; current-runtime, §7) |
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

Status: the global SERIAL pin landed in PR #234 for the library, user and org
leases (`ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01` closed); the generation-fenced
final batch (`ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`) remains open.


### Confirmed E1 / PRE-GC dependency from G4 cross-audit (2026-10-02)

P1: reachable publication repair can promote permanent fs: references without
binding them to the current physical life or a non-COMMITTED D. E1 must exercise
reachable HEAD -> temporary zero -> COMMITTED D1(P1) -> late repair and prove
that no new P1-dependent durable reference can appear after D1. This blocks
X1 closure / A1 / GC ON until structurally closed or activation fails closed;
it does not block GC-OFF G4/G5 development. A last refs==0 recheck in recovery
does not eliminate the post-check publication race.

P2 follow-up: an already-authorized delayed PUT can recreate retired K1 bytes.
Characterize physical resurrection/leak at E1/PRE-X1. The separate greenfield
evaluation of empty-state legacy recovery remains recorded in
GC-G4-IMPLEMENTATION-PLAN.md. The G4 plan contains the confirmed source chain
and the current integration/monitoring corrections.
