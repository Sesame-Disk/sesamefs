# E1 — Full-GC publication and recovery evaluation

Status: ACTIVE on codex/e1-full-gc-publication-recovery-evaluation.
Matrix frozen before E1 runtime edits: 2026-10-03. Base main@eabd93bee includes merged G5 PR #248. Scope: supported greenfield deployment with one compatible release. Destructive GC remains OFF.

## Contract and disposition rules

Evaluate current main's full G1→G5 executor and every current writer/repair funnel able to publish a block reference. X1 invariant: once exact physical life P1 has committed D1 (and separately after terminal retirement), no legitimate current-version operation may durably add a P1 reference or publish HEAD with reachable content depending on P1.

A surviving S3 byte alone is not a logical reference. Inspect exact canonical P/D lifecycle and durable root, (storage_class, storage_key), block_references, commit/HEAD/tree ancestry, bytes, repair rows. Read with production EACH_QUORUM/SERIAL where used. Settle ambiguous writes as UNKNOWN; timeout alone proves neither result.

Run this matrix before runtime edits. A counterexample must traverse existing handler/worker/retry/repair/upload code and leave a durable post-D P1 dependency. Record source SHA, exact P/D/referrer, before/after rows, request result, consistency, actor DC, restart, and object state. Missing refs, leaked bytes or retained repair alone do not falsify X1. Non-reproduction alone does not close a W2 row: require positive source exclusion proof or an existing tested fail-closed gate. On RED make the smallest fix, preserve RED evidence, prove GREEN, then run relevant crash/restart and consistency controls.

Use X1 §4 states: CLOSED-EVIDENCE requires positive exclusion under the supported contract; CLOSED-FIX requires a fix plus evidence; CLOSED-GATED requires a named tested fail-closed mechanism. Else OPEN. Do not declare X1 CLOSED while any applicable §4 W2 row is OPEN/UNKNOWN. X1 CLOSED does not activate GC.

## Frozen matrix

Common real-service harness: install exact P1; create/remove/move its legitimate reference through the named current funnel; let real scanner/queue/claim go through PREPARED→COMMITTED; preserve G5 root. Pause/release the writer at the actual boundary. Restart when specified. Each leg includes a no-GC success control. All rows start UNRUN. Existing pre-HEAD PASS does not close post-D/R31 rows.

| ID | Funnel | Counterexample to attempt | RED only if |
|---|---|---|---|
| E1-01 | GC/recovery baseline: candidate→claim→PREPARED→COMMITTED→retire→G5 recovery | Exercise the production worker and MinIO; kill/restart after root, COMMITTED, canonical retirement, S3 delete and mapping/root terminal cleanup. | Recovery creates/reopens P1 liveness or D commits with an authoritative live dependency. Record K1/K2, exact idempotence. G4/G5 legs are controls. |
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

## Topology, exclusions and exit

One stack at a time. Start with single-DC Cassandra plus real MinIO for decisive writer/recovery races. Add isolated 3DC only when actors differ by DC or SERIAL/EACH_QUORUM visibility is material; name the assertion it proves. Use healthy Cassandra 5.0.9, RF1/DC and dedicated MinIO. Record exact crash point/DC/read levels. G4/G5 are controls; repeat their shared executor only if modified.

No runtime edit before this matrix is frozen in a base-referenced commit. If GREEN, close only rows supported by positive §4 evidence or an extant tested gate; else OPEN and X1 stays open. On RED fix minimum cause and rerun exact leg, regression, real Cassandra/MinIO, crash/restart and required DC leg. Preserve RED/UNKNOWN/incomplete results.

Out of scope: G5 clock-health/scheduler, stale-claim settlement, Phase 5 shared-fs_object cascade, Phase 6 execute-time TOCTOU, full PC-D1B.5, A1 startup gate, GC activation, future funnels, speculative coordinator/scheduler redesign. Track separately unless evidence proves the exact post-D reachable-P1 violation on supported code.

Final audit: full Go, E1-required integration, race for changed packages, normal/integration vet, decisive single-DC legs and required 3DC, all sequential in Docker. Re-audit complete diff. Update this matrix, X1/KNOWN_ISSUES, CURRENT_WORK and evidence docs. E1 may recommend X1 CLOSED only if every applicable §4 row has valid disposition and no current supported funnel publishes P1 after D. GC stays OFF; PRE-GC/A1 remains distinct.