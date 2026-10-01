# Library trash lifecycle contract

**Status:** normative. Closes `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`.
**Supersedes:** the lifecycle redesign proposed in PR #240 (closed unmerged). That PR
treated the lifecycle as broken; it is not. The only real gap was the two-statement
window between a lease fence and an unconditional final write, closed here.

> A library may be destroyed only while its canonical `libraries.deleted_at` still
> equals the exact trash generation `D` that authorized the destruction. Restored or
> superseded work is stale and stops without destroying anything.

## 1. Lifecycle

```text
ACTIVE ──soft delete──▶ TRASHED(deleted_at = D, marker deleted_libraries = D)
                           │
                           ├─ restore (any time while the canonical row exists)
                           │
                           ├─ permanent delete (API)  ── purge_requested_at = now
                           │
                           └─ TrashRetentionDays elapse
                                   │
                         GC Phase 13 enqueues library_cascade(identity = D)
                                   │
                         worker purges D, unless D is no longer current
```

- Phase 13 (`scanExpiredDeletedLibraries`) enqueues a marker when `deleted_at` is
  older than `TrashRetentionDays`, or immediately when `purge_requested_at` is set
  by a permanent delete. The cascade item carries `D` as its identity and is
  deduplicated by it.
- The worker (`processLibraryCascade`) checks the marker against `D` twice: before
  it acquires the library hard-delete lease and again once it holds the lease. A
  `null` marker (restored) or a different one (`D'`, re-trashed) makes the item
  stale.
- Guarded cascade children (commits, fs_objects) apply the same rule to the marker.
  If the marker is gone, they drain only when the canonical row is also gone.

## 2. The boundary: one conditional cell

Three writers can end a trash generation. All of them hold the shared library
hard-delete lease (`gc_library_hard_delete_locks`), and all of them fence it right
before their final write. The fence is not enough by itself. A holder can renew, pause
past the stale threshold (3 × 30 min), lose the lease to a stale takeover, and
then resume.

So the final write of each writer is a global-SERIAL LWT on the canonical
`libraries.deleted_at` cell, conditioned on the generation it observed:

| Writer | Final write | Loses when |
|---|---|---|
| GC cascade (`CassandraStore.HardDeleteLibrary`) | `DELETE FROM libraries … IF deleted_at = D` (`DeleteCanonicalLibraryAtGeneration`) | the row exists under another generation → the item finishes as stale |
| API permanent delete (`hardDeleteLibraryRowsFn`) | same statement | the row exists under another generation, or is already gone → `409 library is no longer in trash` |
| Restore (`restoreDeletedLibrary`) | `DELETE deleted_at, deleted_by FROM libraries … IF deleted_at = D` (`ClearCanonicalLibraryGeneration`) | the row was purged or changed → `library is pending permanent deletion` |

Every Paxos round on that cell decides one winner. The losing writer changes nothing
on the canonical row, however long it paused. A conditional cell delete cannot
upsert, so a losing restore can no longer recreate a partial row.

The lease is kept. It still provides mutual exclusion in the normal case, keeps
restore out while a cascade enqueues children, and keeps the stale takeover's
liveness. It just stops being the only thing standing between a paused holder and
its final write.

Both LWTs use `db.LibraryHeadSerialConsistency`, because a whole-row delete of
`libraries` competes with the HEAD authority domain. They are inventoried in
`pc0ExpectedHeadAuthorityGuards` and `pcd1b4ExpectedLifecycleStatements`.

## 3. Marker invariant and write order

Cascade children trust the `deleted_libraries` marker as their generation authority.
The protocol therefore keeps one invariant:

> A marker at `D` implies the canonical row is at `D` or absent.

A marker that outlives a restored canonical row would let guarded children delete
the content of a live library. Each writer therefore orders its writes as follows:

- **Restore** snapshots the marker under the lease, then deletes it (together with
  the aggregate reconciliation request, which recomputes from canonical rows and is
  harmless if restore then loses) *before* its CAS. After winning, it refreshes
  `updated_at` and the admin read models. If it loses and the canonical row is
  confirmed absent, it puts the snapshot back. Otherwise a restore that paused while
  a permanent delete won, and that then removed the marker the delete had rewritten,
  would leave nothing for the GC to reclaim. A marker over an absent row is allowed
  by the invariant, and an extra cascade pass over a purged library is idempotent.
- **GC cascade and permanent delete** run their CAS *before* the follow-up batch.
  The batch removes `libraries_by_id`, the policies and the read models, and either
  removes the marker (GC) or rewrites it with `purge_requested_at` (permanent delete).
  A losing CAS never writes the marker.
- **Soft delete** writes the canonical row and the marker in one logged batch
  (unchanged).

## 4. Crash and ambiguity analysis

| Interruption | Durable state | Recovery |
|---|---|---|
| GC after winning the CAS, before the follow-up batch | canonical absent, marker `D` | The item retries. The CAS finds no row (`absent`) and the cleanup batch finishes idempotently (read models via the deleted-projection fallback). |
| GC CAS with an unknown outcome | either | Error → retry → same as above, or a fresh CAS |
| Permanent delete after winning the CAS, before the batch | canonical absent, soft-delete marker `D` without `purge_requested_at` | Phase 13 picks the marker up after `TrashRetentionDays` and the GC cascade finishes the cleanup. Reclamation is delayed, nothing is lost. |
| Restore after removing the marker, before its CAS | canonical at `D`, no marker | The library stays in trash (listed, restorable, permanently deletable) but is not auto-purged until one of those actions happens. This is the safe direction. |
| Restore after winning the CAS, before the read-model batch | canonical active, no marker | The library is live. The admin read model still shows it trashed until the next library write refreshes it; aggregate reconciliation was already requested. |
| Restore loses its CAS after removing the marker | canonical absent, marker re-inserted from the snapshot | The GC cascade reclaims normally. If the permanent delete had set `purge_requested_at` after the snapshot was taken, the reinstated marker may lack it, which only delays reclamation to `TrashRetentionDays`. The reinstatement is best effort: if it fails, content is left unreclaimed, never deleted. |
| Restore CAS with an unknown outcome | either | A SERIAL re-read settles it under the lease: a present row with `deleted_at` cleared is this restore's own commit. |

## 5. Known residual: client vs. Paxos timestamps

The generation LWTs carry Paxos (Cassandra-clock) timestamps. Soft delete and the
follow-up batches still carry client timestamps. When a transition follows the
previous write to the same cell more closely than the client↔Cassandra clock skew,
the later write can be shadowed. Real transitions are seconds to days apart and
production clocks are NTP-synced to milliseconds. A local Docker Desktop/WSL stack
measured **~240 ms** of skew, which is why the integration tests seed their trash
rows `USING TIMESTAMP` at the trash time. Making soft delete the same conditional
write would remove the mixed domain entirely. That is a separate decision and is
not part of this change.

The hard delete's row tombstone now carries a Paxos timestamp too. On that skewed
local stack, host-run integration tests that write a library with client timestamps
and hard-delete it milliseconds later (PC-D1B.4 R9/R9g/R9i, the lease TTL test)
fail intermittently. Run inside the compose network, where the client shares the
Cassandra VM clock, the same tests pass consistently:
`docker compose run --rm --build --no-deps go-integration-test /bin/sh -c 'go test -tags integration …'`.

## 6. Evidence

Unit (MockStore, `internal/gc/library_trash_boundary_test.go`):

- `RestoreAfterFenceWins`: the worker fences, a restore steals the lease and wins,
  and the worker's final write loses. The library, its commit and its fs_object survive.
  **RED on `main`**: the cascade deleted the library row, `commit-1` and `fs-root`.
- `RestoreBetweenFirstCheckAndLeaseIsStale`: caught by the under-lease re-check
  (already true on `main`).
- `SupersededGenerationIsStale`: a `D` cascade leaves a `D'` re-trash untouched
  (already true on `main`).

Integration (real Cassandra, `internal/api/v2/restore_guard_integration_test.go`):

- `TestRestoreDeletedLibrary_LosesToPurgeAfterFence`: restore fences and pauses,
  the GC purges `D`, and restore resumes and must not resurrect the row. **RED on `main`.**
- `TestPermanentDelete_LosesToRestoreAfterFence`: the API permanent delete renews and
  pauses, its lease goes stale, a restore takes it over and wins, and the delete resumes. It
  must neither delete the row nor rewrite the marker. **RED** with the final delete
  reverted to unconditional.
- `TestRestoreDeletedLibrary_LosingRestoreKeepsGCMarker`: a restore that loses to a
  permanent delete while paused leaves the GC marker in place. **RED** without the
  reinstatement (the marker was gone, stranding the content).
- `TestCassandraHardDeleteLibrary_StaleGenerationIsNoOp`: the GC store side.

Mutation checks, each turning the suite red: dropping the generation condition from
the hard delete or from restore, ignoring a lost CAS in the worker or in the
permanent delete, and making the mock hard delete generation-blind.

## 7. Out of scope

The org and user lifecycles are not changed. The org purge is entered through its own
`BeginOrgPurge` CAS, and the user side is tracked in
`ISSUE-GC-USER-HARD-DELETE-RESTORE-SERIALIZATION-01`. The library deletes that the
org cascade performs go through `cascadeDeleteLibrary`, so they cross this boundary.
