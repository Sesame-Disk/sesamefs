# G5: bounded recovery scheduling and operational convergence

Base: main b57f5ce69, merged #247. Greenfield first production; GC remains OFF.

## Contract

A current-protocol COMMITTED (P,D) must converge without queue, pending item,
scanner/day cursor or by-day projection. Recovery retains exact G4 retirement,
published lifecycle, fresh topology, EACH_QUORUM reload and org/class/key checks.
No logical-reference reauthorization, old-data migration or empty-state recovery
fallback is part of G5. PREPARED remains metadata-only; UNKNOWN fails closed.

The per-tick page/row bound is unconditional; eventual cycle completion and
revisiting retained roots assume advancing application clocks with a bounded,
observed fleet skew, finite claim production in finite time, and successful
future discovery/checkpoint ticks. `Until` is an event-time upper key, not an
insertion snapshot: roots published later with `After < key <= Until` can join
the active cycle; arrivals at or behind `After` wait for wrap. Once new claim
timestamps advance beyond `Until.ClaimedAt`, only the finite set of earlier
claims can still enter its range. This is no fixed wall-clock revisit bound
and makes no unconditional fairness claim under arbitrary clock regression,
skew or indefinitely backdated claim production. Synchronized clocks (NTP or
equivalent), observable skew bounds and a clock-health activation procedure
are PRE-GC operational prerequisites; G5 implements no clock-health gate.
See `ISSUE-GC-ROOT-CYCLE-CLOCK-ASSUMPTION-01` in KNOWN_ISSUES.md.

## Implementation

1. Execute canonical current-protocol recovery directly from the non-expiring
   fixed-bucket roots. Factor the existing exact G4 executor; remove the day walk,
   90-day execution horizon and empty-state destructive branch. Repair by-day
   projection opportunistically, never as prerequisite for discovering work.
2. Replace opaque Cassandra paging state with an exact clustering-key seek token.
   Each bucket visits at most one bounded page per tick, checkpoints only after
   the page is attempted, advances past failed rows and wraps after a finite
   cycle under the clock/progress assumptions above. The captured upper key
   excludes later claims once their timestamps pass the cutoff, but does not
   freeze the set of roots inserted in-range. Neither a missing/deleted root nor
   restart invalidates
   the token. Corrupt/missing checkpoints restart discovery, never supply authority.
3. Reproduce stale-claim settlement on the same P, with worker B crashing before
   PREPARED/root publication. Determine whether root scheduling closes it. If not,
   explicitly resolve its G5/PRE-GC classification and document the real evidence;
   no superficial second claim read is an atomic fix.
4. Update CURRENT_WORK and the critical-path/debt records with G4 complete,
   G5 in development, E1 next. Preserve the E1 late-publication and late-K1-PUT
   activation requirements. Inventory obsolete fixtures while moving recovery
   expectations to the current protocol; retain all negative authority checks.

## Required evidence

- RED before implementation: COMMITTED older than 90 days; lost/corrupt/future
  day cursor; missing/stale projection; blocked prefix exceeding page size.
- Real Cassandra/MinIO: productive handoff, remove queue/pending/discovery,
  referenced P2 present, bounded repeated ticks and fresh-worker restarts;
  K1 absent, K2 bytes/metadata/refs preserved, D1 TERMINAL, exact roots cleared.
- Fairness: repeated failed prefix, multiple pages/buckets, deletion of seek row,
  concurrent inserts before/after seek, finite-cycle wrap and restart. Bound work
  per tick; failed checkpoint and cancellation retain unprocessed work.
- PREPARED abort/promotion without storage; missing canonical/published lifecycle,
  unknown/empty state, changed identity, topology refusal, LWT/S3 ambiguity and
  post-delete crashes remain fail closed and retry the same exact K1.
- Mandatory G5 evidence gate in TestMain and both supported Compose Go runners,
  including a negative omission control. All execution and runners use Docker/Bash.
- Final audit: full Go tree, full required integration tree, DB/GC race, normal
  and integration vet, API/OIDC and sync; directed three-DC authority/restart
  coverage where the harness supports it. Review full branch diff, not just G5.

## Exit

A PR with a bounded per-tick recovery guarantee and eventual revisit under the
stated clock/progress assumptions, plan and actual test
records, no greenfield compatibility paths added, clean branch and verified
remote head. No GC activation, X1 closure or production-readiness claim.

## Evidence-driven scope decision

The same-P stale-claim probe reproduced an Absent/new-owner gap before PREPARED.
COMMITTED roots cannot recover it. The attempted retention fix is withdrawn:
retaining candidate_at for a referenced P let a later zero epoch inherit expired
grace. G5 therefore preserves main's referenced settlement behavior; the race
remains OPEN, P1 FOLLOW-UP / PRE-GC. Closure must separate recovery scheduling
from fresh zero-ref eligibility and cannot use a second non-atomic claim read.
G5 closes bounded durable-root recovery, not the pre-root claim lifecycle.

## Historical validation before cross-audit (2026-10-02)

- RED before implementation: four 120-day day-cursor cases and same-P claim
  scheduling loss before PREPARED. The durable-root tests pass; same-P claim retention was later withdrawn (see audit below).
- PASS in Docker: complete Go tree; DB/GC race; normal and integration vet;
  final GC race including auxiliary faults, cancellation and post-claim release.
- PASS: full required integration tree, 625.046s, all evidence gates enabled.
  The package completed successfully. Editing its open host driver to normalize
  LF caused a subsequent wrapper EOF; the stable LF driver then passed required
  real G4/G5 smoke evidence in 8.893s with exit 0.
- PASS: G5 omission control correctly exits 1; API 20 suites, OIDC 25 tests,
  sync 11 tests. Full integration and omission/API/sync phases ran separately.
- PASS twice on isolated Cassandra 5.0.9, RF1 in dc-na/dc-eu/dc-asia and MinIO:
  G4 coexistence and all three G5 evidence tests (old-life scheduling loss,
  bounded poison-prefix/restart convergence, same-P retention at the historical HEAD, subsequently withdrawn).
- FAIL: the additional P3 cross-DC fence test in those combined runs. First,
  INSTALL setup timed out; then claim confirmation at EACH_QUORUM timed out.
  Both were before physical GC. An earlier isolated run passed P3. This is an
  unresolved multi-DC availability/evidence limitation, not a demonstrated G5
  safety regression. The combined three-DC command exits 1; it is not reported
  as a complete PASS. E1 must characterize and resolve this before activation.
- Initial fresh three-DC setup exposed an API/database fixture mismatch and
  incomplete topology declaration; the harness now starts a current-source
  backend against its own database, GC automatic execution OFF. Interrupted
  greenfield migrations were retried with 60s Cassandra timeout. No historical
  deployment data or compatibility migration was used.
- Full diff audited; exact authority, topology, locator, changed-canonical,
  PREPARED and unsupported-state refusals retained. No schema/dependency change.

G5's covered development contract is implemented and validated. This does not
close the full multi-DC matrix, E1, X1, PRE-GC or any activation gate.

Local logs: `tmp/g5-all-unit-second.log`, `tmp/g5-race-vet.log`,
`tmp/g5-gc-final-audit.log`, `tmp/g5-integration-final-driver.log`,
`tmp/g5-stable-driver-smoke.log`, `tmp/g5-omission-negative.log`,
`tmp/g5-api-oidc.log`, `tmp/g5-sync.log`, `tmp/g5-3dc-retry-evidence.log`,
`tmp/g5-3dc-final-evidence.log`. Logs are disposable local artifacts.

All validation drivers are Bash and run Go/application tests in Docker. The
single-DC full integration, omission control and API/sync phases must run in
sequence because their fixture cleanup is scoped to a test process, not across
processes sharing the same development database.

## Greenfield removal inventory

G5 removes the worker's empty-state physical executor, its global-ref fallback,
the physical 90-day/day-cursor walk and unused associated helpers. No old-data
migration or mixed-version recovery route is added.

`GCStore.StartBlockDeleteOrphan` and its Cassandra/mock implementations remain
as the existing publication API exercised by older authority/identity fixtures.
There is no production writer/worker call site. Its removal, together with those
fixtures and related historical names, is a separate cleanup evaluation; it is
not an accepted empty-state executor or a deployment compatibility requirement.
Normal production entry points are Prepare/Commit/Promote and exact recovery.

Progress assumes service and checkpoint writes eventually recover. Persistent
checkpoint failure is reported and retains roots; it can delay a poisoned
bucket's tail until checkpoint persistence is restored. No failed scheduling
operation supplies physical-delete authority or consumes an unattempted root.

## Cross-audit correction and expanded 3DC evidence (2026-10-02)

- Confirmed P1: a later zero epoch inherited expired candidate_at after referenced
  retention. Focused unit RED reached COMMITTED one second after zero, in both
  pre-check and post-claim re-reference branches. With G5 retention withdrawn,
  both branches reject before a new claim until the fresh candidate grace elapses.
- Stale-claim settlement race is OPEN, P1 FOLLOW-UP / PRE-GC. G5 makes no closure
  claim for the pre-PREPARED gap. Mandatory integration evidence now covers fresh
  same-P zero-epoch grace instead of the withdrawn retention behavior.
- Scanner source contract aligned with direct COMMITTED durable-root recovery.
  Referenced-orphan legacy issue is SUPERSEDED / LEGACY-NOT-REACHABLE under
  greenfield v1; historical counterexample retained. Root SERIAL wording corrected.
- Expanded full three-DC suite with 77de4490a source (before orphan fixture correction): unit/cov PASS; integration FAIL (30
  main tests, 885.888s); API 12/20 PASS, 8 FAIL; OIDC 25/25 PASS; real Seafile
  Docker sync 11/11 PASS. G4/all then-current G5 legs PASS. This is not a global
  PASS and does not establish absence of regressions. Two fixture-only fixes
  are committed: isolate bounded G5 from background GC; seed physical recovery
  through current claim/PREPARED/COMMITTED authority (targeted G3 all PASS).
- PC-D1B.1 complete PASS; PC-D1B.3 healthy SERIAL phases FAIL, outage/recovery
  phases PASS; PC-D1B.4 complete retry PASS, including semantic RED controls.
  R12/M34 remain characterized residuals. H1 seed failed before Sync/V2
  windows; final independent attempts after three-coordinator readiness also
  failed in seeding (Sync recv1, V2 recv2), leaving both windows without evidence.
- W2 post-HEAD full 3DC PASS: local blindness, remote HEAD, ancestor advancement,
  unavailable-DC UNKNOWN/retention and durable cursor resume from NA/EU.
  Sync PutBlock provenance full 3DC PASS: blind-NA global fallback, all-cross-DC
  hit cost at N=1/10/100/1000, unavailable Asia fails closed within timeout.
- Revalidation after the correction: full Go tree PASS; DB/GC race PASS; normal
  and integration vet PASS. Required real Cassandra/MinIO G4/G5 PASS (9.758s),
  including fresh same-P grace. Its first attempt failed an EACH_QUORUM timeout
  during bounded mapping cleanup; that failure is retained. Earlier full-suite
  results retain their exact source scope and are not relabeled PASS.

Detailed local record: tmp/full-3dc-validation-report.md. GC remains OFF; E1,
X1/PRE-GC/A1 and the pre-root scheduling follow-up remain open.

Final expanded matrix: [three-DC validation record](./GC-G5-3DC-VALIDATION.md).
W2 RepairGuard, X2/P3 and all four consistency mutation controls completed with
real expected results on corrected runtime. The connected G5 omission control
rejected missing evidence correctly. H1 Sync/V2 seed failures remain unresolved;
no global three-DC PASS or regression-free claim is made. Further availability
and pre-GC characterization remains E1 work.
