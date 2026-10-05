# E1-09 / W2-9 cross-repository publication safety

Date: 2026-10-05. Base: main@975b3057db3e0aefbc09e41e7beeaf0ce68193e7.

## Frozen plan

Characterize current runtime before changing it. One plaintext file, one block,
same organization and block representation, both real AsyncBatchCopy and
AsyncBatchMove -> processSingleItem paths. Copy reads source metadata into a
pending in-memory object; staging persists destination metadata before pub:.

Controls: normal copy/move, retained source fs: during GC, and destination
repair-first protection. Investigate productive source library deletion/purge
and its real worker cascade as an alternative to Phase 5/6. Never delete source
fs: by CQL to manufacture a race. Temporary upload expiry-state controls and
fixture-scoped candidate discovery are explicit controls, not elapsed TTL proof.

Pause after actual source metadata read and before destination pub:. If productive
source cleanup can release all source liveness, drive actual block GC to exact
COMMITTED and separately TERMINAL, then resume. Assert destination HEAD/tree,
exact source/destination fs:/pub:, durable repair, canonical P, lifecycle/root,
K1/bytes, task completion and move source ordering. The RED criterion is durable
reachable destination dependence on retired P1, not endpoint acknowledgement.

GREEN: document only the measured contract. RED: freeze reproducible unchanged
runtime evidence, implement minimal existing exact-P/liveness adapter after
repair and before HEAD, and demonstrate RED -> GREEN and omission sensitivity.
Retries must capture authority per attempt; unknown/error fails closed.

No directories, multi-block/DC, encrypted cross-representation, W2-10,
R31/W2-11..14, Phase 5/6 fixes, coordinator, PRE-GC/A1 or GC activation.

Final validation in Docker: directed characterization, meaningful mutation
control if runtime changes, race repetitions, Go regression/vet and appropriate
standard suite. Audit all changes and claim limits, commit/push and create PR.
## Frozen unchanged-runtime RED

Directed Docker run on production source main@975b3057: integration FAIL
(16.328s). Normal copy/move PASS. Both copy and move publish destination HEAD
and permanent destination fs: after productive source library purge and exact
P1 COMMITTED, and separately TERMINAL/K1 absent. Copy reports task success;
move reports failure later because source library is already gone, but its
destination HEAD/fs: are already published. Task failure is not rollback.

Source deletion used actual owner HTTP soft-delete and permanent-delete routes;
real source-library queue/cascade/processFSObject removed the permanent fs:.
Only source-owned temporary up: was explicitly lapsed. No source fs: CQL delete,
Phase 5/6 scan, synthetic metadata/P/ref/claim or invented GC answer was used.
Completed real source SELECT response was paused before caller continuation.
Real EACH_QUORUM two-zero reads/full repair scan and exact COMMITTED/root,
TERMINAL/K1 deletion checks reuse the prior verified worker helpers.

Raw log: external $TEMP/sesamefs-e19-red.log. The safety assertions fail on this
revision; the next commit must make them pass. Existing HTTP normal copy/delete
regression already exists; the report's "never measured" claim is narrowed to
this previously untested pre-pub purge/retirement schedule.

## Fix and measured contract

Plan commit e3aa03c3b; unchanged-runtime RED frozen in d8153a52c. Cross-repo
now captures every distinct canonical source placement per destination attempt
with bounded LOCAL_QUORUM reads, before staging. It verifies that the destination
staged block set equals the captured set, then uses the existing final exact-P
primitive after durable repair and immediately before destination HEAD. Missing,
changed, blocked or malformed placement, query failure and unknown authority
cannot authorize HEAD. A failed final gate cleans its own pub:/pending rows and
repair. HEAD-conflict retry re-reads source and captures authority again.

No new up: is required for this measured fix: source fs: protects the borrowed
read until it disappears, and destination pub: plus non-expiring durable repair
protects the final check -> HEAD interval through the existing W2-0 protocol.
The early gap is permitted to lose to GC; original exact-P then rejects. The
repair-first control genuinely expires the actual pub: after source purge,
executes the productive GC candidate/guard, preserves P1/K1 and the candidate,
and continues publication. This is not a claim that a TTL-bound pub: alone is
continuous protection, nor a new repair/renewal design.

A race detector additionally reproduced task progress fields read after the
TaskStore read lock was released. GetTaskProgress now takes a value snapshot
under that lock; a concurrent-update unit regression validates consistent
responses under -race. This is the same asynchronous funnel's progress surface.

Fourteen required names (copy and move for each row):

| Schedule | Required result |
|---|---|
| normal | Source fs: retained; destination HEAD/paired-canonical metadata/permanent fs:, exact P1/bytes including real destination HTTP download, repair-write -> fs-write -> repair-delete. Explicit skip replay preserves destination HEAD/P. |
| committed | Pause actual source SELECT; owner HTTP soft-delete and permanent-delete, real library/fs_object cascade removes source fs:; actual block worker zero-proof/repair scan reaches COMMITTED and retires canonical P1. Continuation rejects before destination HEAD. |
| terminal | Same schedule plus exact real recovery to TERMINAL, absent K1/orphan/root before continuation; destination HEAD unchanged. |
| captured-committed | Pause completed actual physical placement SELECT: retain its P1 response while productive source purge and GC commit D. Late pub:/repair acquisition does not defeat final exact-P; reject and settle attempt. |
| captured-terminal | Same after exact physical retirement; canonical absence cannot be mistaken for fence clearance. Reject without destination reachability. |
| repair-first | Pause completed durable repair INSERT, purge source productively, shorten existing pub: TTL via reference API and observe real expiration. Productive GC guard preserves candidate/P1/K1 with no D; resume safe destination settlement. Copy succeeds; move reports later source absence while safe destination persists. |
| head-conflict | Actual competing empty-file CreateFile wins destination HEAD while copy is paused. First destination CAS loses; second attempt re-captures P, preserves competing tree content and settles. Source move HEAD occurs after observed destination fs: settlement. |

The handlers use actual owner Gin contexts, actual permission middleware and
real Cassandra/SILO IO; library purge uses the existing external HTTP owner
routes. The async task and productive helpers are used, not direct simulation
of processSingleItem outcomes. This does not certify token/auth middleware or
external routing for the in-process batch requests. Query observers pause
completed real responses; their rows/results are never replaced. Scoped worker
queue adapters filter source metadata or exact block candidates, not authority.
Source upload up: removal is an explicit expiry-state control, not elapsed TTL
proof; repair-first pub: expiry is actual Cassandra TTL observation. Worker
fixture grace/discovery controls do not certify production discovery timing.

After rejected COMMITTED legs, exact recovery deletes K1 during teardown;
TERMINAL replay remains idempotent. Successful fixtures remove only owned refs,
expiry projections, repairs and exact physical objects. No production fs: is
manually erased to reach zero; reference deletion in fixture teardown occurs
only after outcome assertions/task completion.

Disposition: **CLOSED-FIX for measured single-file/single-block plaintext,
same-org/same-representation cross-repo copy/move pre-HEAD publication**.
E1-09 is PARTIAL; W2-9 overall stays OPEN for unmeasured directories, multi-block,
multi-DC, encrypted cases, other concurrent publication boundaries and shared
post-HEAD/R31. W2-5 overall, W2-10, W2-11..14, E1/X1, Phase 5/6, coordinator and
PRE-GC/A1 are unchanged. Production GC remains OFF. Move atomicity/rollback
when the source disappears after safe destination publication is not changed.

## Validation

- Unchanged runtime: normal copy/move PASS; four genuine unsafe reachable
  COMMITTED/TERMINAL continuations FAIL (16.328s integration).
- First fix: all twelve initial directed names PASS (26.408s).
- Initial -race: safety checks pass, but exposes existing async progress races;
  fixed with the locked value snapshot, not by suppressing the detector.
- Expanded required fourteen legs and gate-wiring contracts PASS (26.418s),
  including productive HEAD-conflict retries and explicit skip replay.
- Go short/coverage, ten concurrent progress/authority unit race repetitions,
  normal vet and integration vet PASS. An initial test-only enum formatting
  compile/vet error was corrected before these checks passed.
- Isolated runner omission of only validateCopiedBlockPublication restores all
  four captured-P COMMITTED/TERMINAL REDs (10.259s). Source capture, staging,
  repair and HEAD remain productive. Runtime restored by trap; host/runner
  batch_operations.go SHA-256 match. The first omission script failed to compile
  due to an unused local; only the corrected, compiled counterexamples count.
- Ten required -race repetitions: PASS (289.247s), all 140 named legs and
  completeness/gate contracts. After adding real destination HTTP byte-download
  assertions, final Go source passes three more -race repetitions (93.393s),
  all 42 named legs; no reported data races.
- Required copy/normal-only filter: expected FAIL, thirteen other names missing.
  Required unavailable API/Cassandra: expected bootstrap FAIL before tests run.
- Standard go-all-test command on final Go source: PASS, integration 502.758s,
  all 20 API suites and 25/25 OIDC checks. The three dev nodes were rebuilt and
  redeployed with this production source before external HTTP validation.
- Independent owned-artifact check: PASS. All 196 keys from the final race,
  byte-download race and standard integration runs were already physically
  absent, with canonical rows, refs and owned expiry projections absent. Across
  245 exact logged fixture keys, 11 earlier RED/harness objects were cleaned
  only after verifying canonical/refs absence. No unrelated key was enumerated
  for deletion. Temporary maintenance test removed from runner; it is not in Git.
- Final normal and integration vet, gofmt and diff whitespace checks PASS.
  Final diff audit: no unresolved introduced P0/P1/P2. Wider open ledger rows
  and move's partial-operation semantics are not closed by that statement.

Logs are outside Git: $TEMP/sesamefs-e19-{red,green,race-initial,
final-directed,unit-vet-final,omission-final,race-final,download-race,filtered,
unavailable,full-suite,owned-artifacts,vet-final,final-images}.log.


Host, final validation runner and final test image agree on these Go SHA-256s:

| Source | SHA-256 |
|---|---|
| internal/api/v2/batch_operations.go | 14006009ee2c55f810628a232a0d1e086ebd2165bb0ad6f5723a9ac3578418d4 |
| internal/api/v2/cross_repo_publication.go | 8395ca6009ecde805979a572dd39ff160e10c72ced9fe2413139ee2bddcf5161 |
| internal/api/v2/cross_repo_publication_test.go | 3a846b9770f4ea4e4d0dae1f2a1be0e24397fe4c8f57c98187de6ab29409f018 |
| internal/integration/e19_cross_repo_test.go | 194e470119f320505d640fee65eeac42f6b46af51e5cc96aba4bfd814caa130f |
| internal/integration/integration_test.go | 6b8fa8dc4f4123716a7829ae4e92f9e02952e2399017d77e2a9449ff91739386 |

Test image: sha256:7ee5fadc31d612a931d8192dacc2b274d4920d72948541d165643d28e2827eb0.
Backend image: sha256:24561767c85e1895ae868fb6495bb7dc05f52018f08eb943e4ad497e6d793db5.
All three deployed dev nodes' /app/sesamefs SHA-256 matches
c01c149f6387ef0e43107536206cd155e5b6ed73f7cff716192b8f6104195144.
Cassandra/SILO evidence is single-DC; no optional 3DC phase is certified. Local
GC uses the existing dev configuration, including its background participant;
production activation/configuration and DB/GC runtime are unchanged.
