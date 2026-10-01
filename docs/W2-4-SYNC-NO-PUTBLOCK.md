# W2-4: Sync commit without PutBlock provenance

**Base:** `main@1a8f1e77ced12bb30f6430e0806242cff25c5f39`
**Finding:** current-runtime W2 violation reproduced.
**Disposition:** `OPEN`; this investigation records and reproduces the defect. It does not implement the proposed fix.

## Result

The sequence in W2's criterion is reachable:

```text
web block upload -> Sync CheckBlocks says present -> PutCommit + RecvFS, no Sync PutBlock
-> web-session up: expires -> GC sees zero at EACH_QUORUM and commits D(P)
-> Sync stages pub:/repair and publishes HEAD -> fs: is promoted for P
```

The test removes only the fixture's TTL-bound references to model expiry; production TTLs are unchanged. Cassandra executes the real reference read, claim, PREPARED orphan write, and COMMITTED LWT. The test invokes Sync's production `UpdateBranch` handler and then reads Cassandra to confirm that HEAD reaches the new file and `fs:` is promoted. The test also asserts that `up:sync:<repo>:<block>` was absent before and after `CheckBlocks` and `RecvFS`.

The test covers both a normal Seafile SHA-1 block ID and, after complete physical retirement of P, the canonical SHA-256 ID. It exercises direct fast-forward and the concurrent-update auto-merge path. GC committing D before the publication-attempt stage and after initial readiness but before durable repair acquisition both produce D(P)+HEAD. If repair is already queued, its EACH_QUORUM guard prevents D. The defect therefore lies in the window before that repair, combined with the fact that an unprovenanced block receives no exact-placement check before HEAD.

This is distinct from W2-3's expired Sync `PutBlock` provenance: these fixtures never call the Sync `PutBlock` endpoint. The bytes are materialized through the supported web block-upload route, and real Sync `CheckBlocks` resolves their SHA-1 alias and canonical SHA-256 ID as present. The reachable caller is client dedup/reuse without a Sync upload.

## Source trace

1. `CheckBlocks` resolves each legacy SHA-1 to canonical SHA-256 and checks canonical storage. Its response does not register Sync publication liveness.
2. `RecvFS` validates and stores immutable file/tree metadata. It does not register block references.
3. `stageSyncCommitBlockDelta` resolves the target's added files and stages attempt-local `pub:` references.
4. `prepareSyncCommitBlockPublicationReadiness` calls `syncCommitProvenancedBlockIDs`. With no observable `up:sync:<repo>:<block>`, the resulting set is empty and the function returns no placements, skipping placement resolution and `ValidateBorrowedFSPublicationAuthority`.
5. The direct and auto-merge paths queue `published_block_reference_repairs` only after this readiness step. A repair already present blocks the GC EACH_QUORUM destructive probe; attempt-local `pub:` alone is TTL-bound and can lapse.
6. After the queue call, exact-P revalidation covers only the captured PutBlock-provenanced placements. With this row empty, the HEAD CAS can publish after D(P). Post-HEAD settlement then promotes `fs:` for the now reachable file.

`CheckBlocks` returning present says that bytes exist at that instant. It does not promise those bytes remain alive through later metadata receipt, publication staging, and HEAD. A pre-existing `fs:` can protect a canonical reused object only while that reference remains live; W2-4's reproduced sequence uses the real expiring web-session pin and does not rely on a hypothetical caller.

## Proposed smallest fix

Keep the existing provenance rule for *renewing* `up:sync:`: never manufacture a Sync upload pin from a commit delta. Independently of that scope, resolve the exact `(block_id, storage_class, storage_key)` placement for every canonical block in the added-file delta, acquire the existing durable per-file repair intent, and revalidate every captured placement after acquisition immediately before HEAD. Apply the same union to direct and auto-merge promotion.

This uses the existing repair guard and exact-placement validator. The ordering closes both interleavings: if D(P) commits first, the final check rejects publication; if repair becomes visible first, GC's EACH_QUORUM probe cannot establish zero. It adds no new pin, table, protocol field, coordinator, or GC behavior. Follow-up implementation must prove the race with a RED test first and retain the repair on uncertain HEAD settlement under the existing ownership rules.

## Evidence

`internal/integration/w2_sync_no_putblock_test.go` contains ten named Cassandra/MinIO legs. The canonical-ID leg is a separate wire identity after physical retirement; it does not depend on GC removing the SHA-1 mapping:

| Writer path | Writer first | GC D before stage | GC D before repair | Repair first | P fully retired before HEAD |
|---|---:|---:|---:|---:|---:|
| Direct HEAD | succeeds | **D(P)+HEAD** | **D(P)+HEAD** | GC postponed | **D(P)+HEAD** |
| Auto-merge | succeeds | **D(P)+HEAD** | **D(P)+HEAD** | GC postponed | **D(P)+HEAD** |

`SESAMEFS_W24_ASSERT_SAFETY=1` changes the reproduced violation into an assertion failure, providing RED evidence for the next fix. `SESAMEFS_REQUIRE_W24_CHARACTERIZATION=1` makes unavailable Cassandra, skipped coverage, or a missing named leg fail the command. `scripts/w2-sync-no-putblock-validation.sh` starts a fresh project-scoped Docker stack and runs both commands in containers.

Limits: this is single-DC real Cassandra/MinIO evidence for this row's source-order race, not three-DC evidence, a W2-5 conclusion, R31 closure, G4 evidence, or X1 closure. The separate R31 and post-HEAD requirements remain open.
