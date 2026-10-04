# E1 — Full-GC publication and recovery evaluation

Status: ACTIVE; E1-3 on codex/e1-3-late-publication-pre-d-proof, following merged #251.
Matrix frozen before E1 runtime edits: 2026-10-03. Base main@eabd93bee includes merged G5 PR #248 and the pinned SILO backend from PR #249. Scope: supported greenfield deployment with one compatible release. Destructive GC remains OFF.

Tested storage backend: `docker.io/pgsty/silo:RELEASE.2026-09-16T00-00-00Z`, a MinIO-compatible S3 backend. The Compose service remains named `minio`; this ledger does not claim tests against the archived MinIO server image.

## Contract and disposition rules

Evaluate current main's full G1→G5 executor and every current writer/repair funnel able to publish a block reference. X1 invariant: once exact physical life P1 has committed D1 (and separately after terminal retirement), no legitimate current-version operation may durably add a P1 reference or publish HEAD with reachable content depending on P1.

A surviving S3 byte alone is not a logical reference. Inspect exact canonical P/D lifecycle and durable root, (storage_class, storage_key), block_references, commit/HEAD/tree ancestry, bytes, repair rows. Read with production EACH_QUORUM/SERIAL where used. Settle ambiguous writes as UNKNOWN; timeout alone proves neither result.

Run this matrix before runtime edits. A counterexample must traverse existing handler/worker/retry/repair/upload code and leave a durable post-D P1 dependency. Record source SHA, exact P/D/referrer, before/after rows, request result, consistency, actor DC, restart, and object state. Missing refs, leaked bytes or retained repair alone do not falsify X1. Non-reproduction alone does not close a W2 row: require positive source exclusion proof or an existing tested fail-closed gate. On RED make the smallest fix, preserve RED evidence, prove GREEN, then run relevant crash/restart and consistency controls.

Use X1 §4 states: CLOSED-EVIDENCE requires positive exclusion under the supported contract; CLOSED-FIX requires a fix plus evidence; CLOSED-GATED requires a named tested fail-closed mechanism. Else OPEN. Do not declare X1 CLOSED while any applicable §4 W2 row is OPEN/UNKNOWN. X1 CLOSED does not activate GC.

## Frozen matrix

Common real-service harness: install exact P1; create/remove/move its legitimate reference through the named current funnel; let real scanner/queue/claim go through PREPARED→COMMITTED; preserve G5 root. Pause/release the writer at the actual boundary. Restart when specified. Each leg includes a no-GC success control. All rows start UNRUN. Existing pre-HEAD PASS does not close post-D/R31 rows.

| ID | Funnel | Counterexample to attempt | RED only if |
|---|---|---|---|
| E1-01 | GC/recovery baseline: candidate→claim→PREPARED→COMMITTED→retire→G5 recovery | Exercise the production worker and SILO; kill/restart after root, COMMITTED, canonical retirement, S3 delete and mapping/root terminal cleanup. | Recovery creates/reopens P1 liveness or D commits with an authoritative live dependency. Record K1/K2, exact idempotence. G4/G5 legs are controls. |
| E1-02 | Master late-publication race | Hold a valid writer/repair; let prior fs:/pub:/up: lapse; run full GC to COMMITTED D1(P1); resume at its real publication step. Repeat once D is TERMINAL. | Durable P1 reference or reachable HEAD/commit/tree after D, including accepted success leaving that state. Priority one. |
| E1-03 | W2-1/2 CreateFileFromBlocks, SessionUpload, BorrowedFS/dedup | Pause around ensureCommitBlockOwnLiveness, final validateCommitBlockPublicationFences, pub: stage, HEAD CAS and settlement. Cover owned up: and foreign fs:; expire only real TTL. | Post-D P1 dependency. Record 409/unchanged HEAD controls. Separate pre-HEAD proof from R31. |
| E1-04 | W2-3 Sync PutBlock, auto-merge, retry on another pod | Delay across up: expiry or cross-DC visibility, commit D1, then resume readiness/HEAD/repair from relevant DCs. | Reachable P1 after D. Fail-closed timeout/unchanged HEAD is safe but does not close availability. |
| E1-05 | W2-4 post-HEAD R31; W2-5 RecvFS before PutBlock | Run no-PutBlock repair chain after its pre-HEAD fix; separately trace actual ReceiveFS→commit→later PutBlock. Interleave D at each existing durable boundary and retry. | Late repair creates P1 after D or legal Sync publishes reachable retired P1. Pre-HEAD W2-4 is not R31 evidence. |
| E1-06 | W2-6/6a v2 UploadFile and Office-template CreateFile | Keep exact-placement checks as controls. Pause after HEAD at pending repair settlement, cleanup, renewal; expire pub: by TTL; commit D; restart/resume. | Post-D P1 ref/reachable HEAD. Distinguish UNKNOWN from proven loser. |
| E1-07 | W2-7 SeafHTTP normal/streaming | Use HandleUpload/finalizeUploadStreaming; pause from materialization/up: through stageSeafHTTPPublishAttemptReferences, HEAD, finalizeSeafHTTPPublishedBlockReferences, retry/cleanup. | Post-D P1 ref/HEAD. Record staged/promoted identity and response. |
| E1-08 | W2-8 OnlyOffice callback | Use saveOnlyOfficePendingBlock and actual callback. Pause around stagePendingPublishedFiles, HEAD, repair, cleanup, replay and D. | Successful callback leaves a durable post-D P1 dependency. |
| E1-09 | W2-9 cross-repository copy/move | Use copyFSObjectToLibraryForPublish/processSingleItem. Read source P1; stage destination pub:; race source GC; publish destination; resume promotion and source cleanup. Exercise copy and move. | Destination HEAD/fs_object durably depends on retired P1. Pre-HEAD failure or bytes alone is not RED. |
| E1-10 | W2-10 RevertFile, RevertDirectory, RestoreTrashItem, RevertDirents | Pause actual historical tree path after old fs_object/block read but before new HEAD/ref; commit D1; resume. Retain an extant tree; keep Phase 6 deletion outside this leg. | Handler publishes reachable P1 post-D. If only reachable through Phase 6 or invented state, document and leave W2 OPEN. |
| E1-11 | W2-11 repair pub: renewal after classifier | Pause repairPublishedBlockReferenceRepairVisit between ancestry classification and renewPublishedBlockReferenceRepairLivenessIfPending; let liveness expire, commit D, resume. | Renewal/promotion creates durable P1 after D; preserve ancestry proof. |
| E1-12 | W2-12 known-loser durability | Force real applied=false HEAD CAS; kill request before local cleanup; discover/restart repair around D. Never relabel UNKNOWN as loser. | Unreachable loser is promoted as P1 after D. |
| E1-13 | W2-13 repair discovery scale | Put an actually reachable repair behind controlled current backlog; expire pub: through TTL, advance G5, restart/visit repair. | Reachable P1 is referenced after D. Slowness/retention alone is not RED. |
| E1-14 | W2-14 pub: expiry/zero-ref transition | Expire final pub: with pending repair; run scanner/G5 to D; resume persisted repair. Include no-repair control; never manufacture gap via direct CQL. | Reachable P1 promoted post-D. If durable guard prevents D, capture positive source proof and tested gate. |
| E1-15 | Shared Sync/SeafHTTP/OnlyOffice/cross-repo repair cleanup, cancel, retry | Interleave owner cleanup, dead/unreachable repair, cancellation, renewal, progress-bucket contention, restart and retry at actual boundaries. | An actual HEAD-reachable repair adds P1 after D. Ownerless pub over-retention or irrelevant residue remains a follow-up. |

## Delayed PUT (separate classification)

E1-PUT-01: obtain a real pre-D authorized upload/session for P1/K1; run GC through COMMITTED and full retirement/K1 deletion; deliver held PutBlock/storage write and its real metadata-install, mapping and reference steps. Repeat at worker restart and only a material DC boundary. Inspect exact bytes, canonical P/D, mapping, block_references, reachable HEAD at production consistency.

- K1 bytes alone return: storage over-retention/leak follow-up, not by itself X1 RED.
- Metadata/ref/HEAD reinstalls P1 after D: E1/X1 RED; fix only if reachable through supported current code.
- Upload/install fails closed with no P1 ref: safe; record availability/retry.

Use real authorization/upload/install/mapping; a test-only writer is not a product counterexample.

## Execution ledger — 2026-10-03 (partial; X1 remains OPEN)

The E1 integration additions are test-only and build-tagged `integration`; no
production runtime behavior changed. The final Docker command
`docker compose --profile test run --rm --build go-all-test` exited 0 after
these additions, including Go unit/integration, API and OIDC suites. The normal
runner reports isolated 3DC legs as skipped when they are not enabled; no E1
cross-DC leg is claimed as passed. The full-suite result does not substitute
for the post-D counterexample legs below. The directed E1 cases also pass under
`go test -race`; `go vet ./...` and `go vet -tags integration ./...` pass in the
same sequential Docker runner. An initial race repeat exposed a fixture
assumption: the application G5 recovery worker can finish the same durable root
before the test-scoped visitor. Its verifier now accepts that interleaving only
after confirming the exact terminal class/key certificate, absent exact root
and orphan, absent canonical row, and deleted K1; the race rerun passes.

| Case | Result | Evidence and limit |
|---|---|---|
| E1-PUT-01 delayed physical PUT | PASS for the exercised Sync `PutBlock` path | `TestE1DelayedPutCannotRestoreRetiredPhysicalLife` uses real Cassandra, SILO, the production handler and G5 worker. It pauses the physical write after authorization of exact P1, drives P1 through terminal recovery and K1 deletion, then releases the actual storage PUT. The request returns 200; K1 bytes reappear, but P1 stays absent, the same logical block is rematerialized at a different P2 key, and references contain the upload `up:` row without a P1 `fs:` row. The no-GC control also passes. This is a K1 orphan/over-retention result (the existing [G4 delayed-PUT follow-up](./X1-CRITICAL-PATH.md#confirmed-e1--pre-gc-dependency-from-g4-cross-audit-2026-10-02)), not X1 RED and not closure of W2-3 or the stale-delete ABA issue. |
| E1-02 losing-target repair replay | PARTIAL; row remains OPEN | `TestW2WorkerRepairLifecycle/lateRepairDoesNotStallCommittedDelete` replays the production repair visitor after terminal D for a commit that lost HEAD. The production resumable classifier returns `unknown` without error, and the actual visitor returns the typed `retained` outcome (operational/renewal failures are rejected); the row and its repair-owned `pub:` liveness remain, HEAD is unchanged, and neither P1 nor K1 returns. This does not exercise a repair whose commit is already reachable from HEAD after D, so it is not evidence closing the master late-publication race or W2-11/12. |
| E1-01 and E1-03 | UNRUN in this evaluation | Existing G4/G5 tests are controls only; this branch did not repeat the full crash-point or upload/dedup post-D matrix. |
| E1-04 | PARTIAL; row remains OPEN | The delayed-PUT case above covers only the held physical-write continuation. Sync HEAD, auto-merge and cross-pod retry after D are not evaluated here. |
| E1-05 through E1-15 | UNRUN; rows remain OPEN | Source tracing below records the current funnel shape, not a post-D integration result. |

Source trace for the unrun funnels:

- SeafHTTP normal and streaming uploads stage `pub:` references and durable
  repair before HEAD, then promote to `fs:` after HEAD. This is not an E1
  post-D result; W2-7 stays OPEN.
- Sync `RecvFS` stores the authorized fs-object projection but does not itself
  publish HEAD or create block liveness. HEAD publication is a separate Sync
  step; the existing no-GC `RecvFS-before-PutBlock` integration case is not a
  post-D interleaving. W2-5 stays OPEN.
- `CreateFileFromBlocks`, v2 `UploadFile` and Office-template `CreateFile` use
  staged `pub:`/repair state and exact-placement checks before HEAD in their
  covered paths. Existing pre-HEAD controls are not post-D E1 results, and
  their applicable R31 rows stay OPEN.
- OnlyOffice callback originally had no final exact-P check. E1-4 reproduces
  HEAD/fs: after COMMITTED and TERMINAL P1 retirement and adds the minimal
  exact-P validation after repair, before HEAD. Measured pre-HEAD safety and
  terminal P2 replay are CLOSED-FIX; post-HEAD/R31 W2-8 remains OPEN.
- Cross-repository copy/move in `BatchOperationHandler.processSingleItem`
  copies source fs objects, stages destination `pub:` references and durable
  repair before HEAD, then promotes after HEAD. The source trace does not prove
  safety if stale source metadata is presented after D; W2-9 stays OPEN.
- `RevertFile`, `RevertDirectory`, `RestoreTrashItem` and `RevertDirents`
  rebuild a tree from historical fs objects and update HEAD directly, without
  the pending-publish `pub:`/repair sequence or an exact-placement check. No
  race was run, and source inspection alone does not establish whether the
  required historical object can coexist with retired P1; W2-10 stays OPEN.
- The Sync idempotent-repair path can enqueue a repair for an already-current
  HEAD and intentionally has no retroactive exact-P rejection. This branch did
  not construct that state through a supported current flow after D; W2-3 and
  the applicable R31 rows stay OPEN.

No E1 row is marked CLOSED by non-reproduction. In particular, no reachable
post-D repair was exercised, the restart matrix is incomplete, W2-11..14 remain
open, X1 is not closed, and destructive GC remains OFF. These results authorize
no PRE-GC or startup-gate transition.

## Cross-audit hardening — 2026-10-03

The three test/evidence findings are addressed without production runtime edits:

- Both delayed-PUT and no-GC fixtures clean the current canonical storage key
  before metadata teardown, including rematerialized K2, and their exact Sync
  `up:` referrer. Independent teardown reads verify the physical object,
  canonical expiry tracker and durable by-day expiry projection are absent.
- E1-02 requires the real resumable classifier to return `unknown` with no
  operational error, then requires the actual visitor's typed `retained`
  outcome. Retention alone does not certify UNKNOWN. The test also verifies
  the durable repair identity and its exact repair-owned `pub:` referrer.
- The tested backend is pinned SILO, as recorded above. Historical MinIO
  evidence outside this E1 slice is unchanged.

The additional verifier correction covers a competing G5 worker completing
between recovery-root discovery and the exact orphan read. A clean missing
orphan is accepted only after the existing terminal certificate/object/root
checks pass. `TestW2RecoveryCompletesBetweenRootListAndRead` drives that
interleaving through the real worker; it is a harness regression control,
not closure of E1-01's crash/restart matrix.

Audit validation: PASS, all sequential in a single Docker runner built from
`e0358bd4b` plus this audit-hardening diff. The standard `go-all-test` command
(Go unit/integration, API and OIDC) and normal/integration `go vet` pass; the
integration suite completed in 457.946s. Ten `-race -count=10` repetitions of
`TestE1DelayedPutCannotRestoreRetiredPhysicalLife`,
`TestW2WorkerRepairLifecycle` and
`TestW2RecoveryCompletesBetweenRootListAndRead` pass (56.212s).

Three container-only mutations are RED for their own assertion: removing
cleanup leaves the physical object, canonical expiry tracker and by-day
projection; substituting
an arbitrary DB error yields a `failed` visit instead of `retained`; removing
the new discovery/read fallback loses the committed continuation after the
competing real worker finishes. Source was restored before the final race run;
mutation-only teardown also removes its exact artifacts. No host source was
mutated by these experiments. Optional 3DC/proxy/saturation legs are not claimed
as passed, and no E1/X1 disposition or production GC activation gate changed.

## E1-2 reachable-HEAD gate evidence — 2026-10-03

On main@c1f31f7ec plus the E1-2 test, a real Office CreateFile publication wins
HEAD and stops before permanent-reference promotion. The test verifies the
HEAD/commit/tree/block chain and lets its actual temporary references expire in
Cassandra using a two-second TTL time control through the reference API.
The surviving durable repair yields REPAIR_GUARD_ONLY; the real worker
postpones before destructive handoff. A fresh-session productive visitor then
classifies REACHABLE and settles to the exact fs: without changing HEAD/P1/K1.

This is positive evidence of the existing pre-D gate for the covered funnel,
not a post-D reachable-repair execution. No COMMITTED or TERMINAL D is forced.
E1-02, W2-11/14 and X1 remain OPEN; concurrent renewal/cleanup and other funnels
need separate disposition. The no-GC control, experiment, source exclusion
argument and validation are in [the E1-2 ledger](./E1-2-REACHABLE-HEAD-LATE-REPAIR.md).

## E1-3 late publication / negative repair scan — 2026-10-03

The [E1-3 plan and evidence](./E1-3-LATE-PUBLICATION-PRE-D-PROOF.md) records
two directed PASS orderings for real Office CreateFile/shared repair:
late repair acquisition under an already-held claim is rejected at final
exact-P, leaving HEAD unchanged; reachable settlement during the proof writes
fs: before deleting repair, so a subsequent negative scan is protected by the
second EACH_QUORUM refs read. Its original HEAD/tree/P1/K1 survive without D.
A no-GC writer control succeeds. Driver observation leaves actual query
responses unchanged. This is not closure of every pre-D schedule, W2-11/14,
other funnels or X1, and grants no activation transition.

## Topology, exclusions and exit

One stack at a time. Start with single-DC Cassandra plus real SILO for decisive writer/recovery races. Add isolated 3DC only when actors differ by DC or SERIAL/EACH_QUORUM visibility is material; name the assertion it proves. Use healthy Cassandra 5.0.9, RF1/DC and dedicated SILO. Record exact crash point/DC/read levels. G4/G5 are controls; repeat their shared executor only if modified.

No runtime edit before this matrix is frozen in a base-referenced commit. If GREEN, close only rows supported by positive §4 evidence or an extant tested gate; else OPEN and X1 stays open. On RED fix minimum cause and rerun exact leg, regression, real Cassandra/SILO, crash/restart and required DC leg. Preserve RED/UNKNOWN/incomplete results.

Out of scope: G5 clock-health/scheduler, stale-claim settlement, Phase 5 shared-fs_object cascade, Phase 6 execute-time TOCTOU, full PC-D1B.5, A1 startup gate, GC activation, future funnels, speculative coordinator/scheduler redesign. Track separately unless evidence proves the exact post-D reachable-P1 violation on supported code.

Final audit: full Go, E1-required integration, race for changed packages, normal/integration vet, decisive single-DC legs and required 3DC, all sequential in Docker. Re-audit complete diff. Update this matrix, X1/KNOWN_ISSUES, CURRENT_WORK and evidence docs. E1 may recommend X1 CLOSED only if every applicable §4 row has valid disposition and no current supported funnel publishes P1 after D. GC stays OFF; PRE-GC/A1 remains distinct.

## E1-4 / W2-8 OnlyOffice callback

[E1-4 plan and evidence](./E1-4-ONLYOFFICE-PUBLICATION-SAFETY.md) records
current-behavior RED for a real callback resuming after exact P1 COMMITTED and
TERMINAL retirement. The fix retains the original materialized placement
through durable repair and validates it immediately before HEAD. Normal save,
rejection/cleanup and terminal replay at P2 are covered; expiry is an explicit
state control. Only this pre-HEAD contract is CLOSED-FIX. E1-08 post-HEAD repair,
concurrent cleanup/recovery and the shared R31 matrix remain OPEN.
