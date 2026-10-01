# Library trash lifecycle contract

**Status:** normative. Closes `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`.
**Supersedes:** the lifecycle redesign proposed in PR #240 (closed unmerged). That PR
treated the lifecycle as broken; it is not. The only real gap was the window between a
lease fence and an unconditional final write, closed here.

> A stale trash generation cannot commit the canonical lifecycle transition or authorize
> generation-bound content destruction. A library is destroyed only while its canonical
> `libraries.deleted_at` still equals the exact generation `D` that authorized it.

## 1. Lifecycle

```text
ACTIVE ──soft delete (CAS)──▶ TRASHED(deleted_at = D) ──▶ marker deleted_libraries = D
                                  │
                                  ├─ restore (CAS, while the canonical row exists)
                                  │
                                  ├─ permanent delete (API, CAS) ── purge_requested_at = now
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
- The worker (`processLibraryCascade`) checks the marker against `D` before it acquires
  the library hard-delete lease, and again once it holds the lease. Then, still under
  the lease, it checks the **canonical** generation. A row that exists with
  `deleted_at ≠ D` makes the cascade stale: the worker removes marker `D`
  (`IF deleted_at = D`) and stops. An absent row proceeds, because it means a permanent
  delete or a crashed earlier pass.
- Guarded cascade children (commits, fs_objects) require the marker to match `D`
  **and** the canonical row to be gone before they destroy anything. While the row
  exists they postpone.

## 2. The boundary: one conditional cell

Every transition of the trash generation is a global-SERIAL LWT on the canonical
`libraries.deleted_at` cell, conditioned on the state its writer observed. No
transition is an ordinary client-timestamped write, so neither the timestamps nor a
pause can reorder transitions.

| Writer | Generation write | Loses when |
|---|---|---|
| Soft delete (`softDeleteLibrary`, `CassandraStore.SoftDeleteLibrary`) | `UPDATE … SET deleted_at = D … IF deleted_at = null AND created_at != null` (`SoftDeleteCanonicalLibraryGeneration`) | already trashed → idempotent (it completes a missing marker for the current generation once), or the row is absent |
| Restore (`restoreDeletedLibrary`) | `UPDATE … SET deleted_at = null, deleted_by = null, updated_at = ? … IF deleted_at = D` (`ClearCanonicalLibraryGeneration`) | the row was purged or is at another generation → `library is pending permanent deletion` |
| GC cascade (`CassandraStore.HardDeleteLibrary`) | `DELETE FROM libraries … IF deleted_at = D` (`DeleteCanonicalLibraryAtGeneration`) | the row exists under another generation → the item finishes as stale |
| API permanent delete (`hardDeleteLibraryRowsFn`) | same statement | the row exists under another generation, or is already gone → `409 library is no longer in trash` |

Restore, the GC cascade and the permanent delete also hold the shared library
hard-delete lease and fence it right before their CAS. The fence is not enough by
itself. A holder can renew, pause past the stale threshold (3 × 30 min), lose the lease
to a stale takeover, and then resume. The CAS decides, and the losing writer changes
nothing on the canonical row however long it paused. Conditional writes never create
an absent row. Restore's CAS also stamps `updated_at`, so no ordinary write to the
canonical row follows it.

All generation LWTs use `db.LibraryHeadSerialConsistency`, the `libraries` partition's
Paxos domain. They are inventoried in `pc0ExpectedHeadAuthorityGuards` (the whole-row
DELETE) and `pcd1b4ExpectedLifecycleStatements`.

## 3. Marker and completion order

The marker `deleted_libraries` is a discovery root for Phase 13. It never authorizes
destruction on its own: the worker and the children also check the canonical row. Each
writer runs its generation CAS first and only then touches derived state:

- **Soft delete**: CAS, then one batch with marker `D`, the aggregate reconciliation
  request and the read models, then the aggregate adjustment.
- **Restore**: CAS. Then marker `D` is removed **only while it is still `D`**
  (`DeleteLibraryMarkerAtGeneration`), so a newer generation's marker survives. Then the
  read models are refreshed from the snapshot to the **current** canonical row, re-read
  after the CAS. If the library was trashed again (`D2`) meanwhile, `D2` is published,
  not ACTIVE. If it was purged, nothing is published. Storage is re-added only while the
  row is still active.
- **GC cascade and permanent delete**: CAS, then the batch that removes
  `libraries_by_id`, the policies and the read models, and either removes the marker
  (GC) or rewrites it with `purge_requested_at` (permanent delete). A losing CAS writes
  nothing.

An ambiguous restore CAS is claimed as won only if the restore **still owns the lease**
(a re-fence succeeds) **and** a SERIAL read shows the row present with `deleted_at`
null. A cleared `deleted_at` alone proves that some restore committed, not that this
one did. Anything else fails as unknown and runs no completion
(`settleAmbiguousLibraryGenerationClear`).

## 4. Crash and ambiguity analysis

| Interruption | Durable state | Recovery |
|---|---|---|
| Soft delete after its CAS, before the marker batch | canonical at `D`, no marker | The library is in trash: listed, restorable, permanently deletable. A repeated soft delete (for example a user/org cascade) completes the missing marker once. Until then it is not auto-purged, which is the safe direction. |
| Restore after its CAS, before removing the marker | canonical active (or `D2`), marker `D` | The GC cascade for `D` finds the canonical generation ≠ `D` under its lease, removes marker `D` and stops. Children never destroy while the row exists. |
| Restore after the marker, before the read-model batch | canonical active, no marker | The library is live. The admin read model shows it trashed until the next library write refreshes it. |
| Restore CAS with an unknown outcome | either | Settled only as described in §3; otherwise an error and no completion. |
| GC after winning the CAS, before the follow-up batch | canonical absent, marker `D` | The item retries. The CAS finds no row (`absent`) and the cleanup batch finishes idempotently (read models via the deleted-projection fallback). |
| GC CAS with an unknown outcome | either | Error → retry → same as above, or a fresh CAS. |
| Permanent delete after winning the CAS, before the batch | canonical absent, soft-delete marker `D` without `purge_requested_at` | Phase 13 picks the marker up after `TrashRetentionDays` and the GC cascade finishes the cleanup. Reclamation is delayed; nothing is lost. |

## 5. Known residuals (not changed here)

- **Other ordinary writers of the `libraries` row.** Owner transfer, renames, size
  updates and the like are plain client-timestamped upserts. One that lands after a hard
  delete can leave partial cells (`ISSUE-PCD1B-CONTINUITY-LWT-GHOST-ROW-01` and related
  follow-ups). This change does not convert them; they do not write `deleted_at`.
- **Marker table timestamps.** Marker inserts are plain writes and generation-scoped
  marker deletes are LWTs. A plain insert can only lose to an earlier-timestamped
  delete, which leaves a library unreclaimed (safe direction), never destroyed.
- **Link cleanup before the permanent-delete CAS.** Share/upload links can be removed by
  a permanent delete that then loses its CAS to a restore. This is pre-existing and
  tracked as a follow-up.
- **Local clock skew.** A Docker Desktop/WSL host measured **~240 ms** ahead of the
  Cassandra VM. Host-run integration tests that mix client-timestamped writes with LWTs
  on the same cells (PC-D1B.4 R9/R9g/R9i, the lease TTL test) fail intermittently there.
  Inside the compose network they pass:
  `docker compose run --rm --build --no-deps go-integration-test /bin/sh -c 'go test -tags integration …'`.
  The tests here seed rows `USING TIMESTAMP` in the past for the same reason.

## 6. Evidence

Each test below was RED against the code it fixes and is GREEN now.

Unit (MockStore, `internal/gc/library_trash_boundary_test.go`):

- `RestoreAfterFenceWins`: the worker fences, a restore steals the lease and wins,
  and the worker's final write loses. The library, its commit and its fs_object survive.
  On `main` the cascade deleted all three.
- `StaleMarkerOverRestoredCanonicalIsSettled`: canonical active with marker `D` left
  behind. The cascade is stale, the marker is cleared, and no child is left postponed
  forever.
- `TestSettleAmbiguousLibraryGenerationClear`: an ambiguous restore is not claimed after
  losing the lease, nor while `deleted_at` is set, nor when the row is absent.
- `RestoreBetweenFirstCheckAndLeaseIsStale`, `SupersededGenerationIsStale`: already true
  on `main`, now frozen.

Integration (real Cassandra, `internal/api/v2/restore_guard_integration_test.go`):

- `TestRestoreDeletedLibrary_LosesToPurgeAfterFence`: a paused restore does not
  resurrect a purged row.
- `TestPermanentDelete_LosesToRestoreAfterFence`: a paused permanent delete (real API
  path, stale takeover) neither deletes the restored row nor rewrites its marker.
- `TestRestoreDeletedLibrary_CompletionDoesNotResurrectPurgedRow`: restore wins, pauses;
  `D2` soft delete and permanent delete. The resumed completion must not upsert the row.
- `TestRestoreDeletedLibrary_StaleRestoreKeepsNewerGenerationMarker`: a stale restore
  of `D1` leaves `D2`'s marker in place.
- `TestRestoreDeletedLibrary_CompletionPublishesCurrentGeneration`: after a `D2` soft
  delete during the pause, the completion publishes `D2`, not ACTIVE, and keeps `D2`'s
  marker.
- `TestSoftDeleteThenImmediateRestoreClearsGeneration`: on the skewed host, restore
  right after soft delete reported success while `deleted_at` stayed set (2 of 3 runs).
  With both writes as LWTs it passes regardless of clock skew.
- `TestRestoreDeletedLibrary_LosingRestoreKeepsGCMarker`,
  `TestCassandraHardDeleteLibrary_StaleGenerationIsNoOp`: losing restore and store side.

Mutation checks, each turning a test red: dropping the generation condition from the
hard delete or from restore, the worker or the permanent delete ignoring a lost CAS, a
generation-blind mock hard delete, reverting the permanent delete to an unconditional
DELETE, settling an ambiguous restore without the lease, skipping the worker's canonical
generation check, and an unconditional marker delete in restore.

## 7. Out of scope

The org and user lifecycles are not changed. The org purge is entered through its own
`BeginOrgPurge` CAS, and the user side is tracked in
`ISSUE-GC-USER-HARD-DELETE-RESTORE-SERIALIZATION-01`. The library deletes that the org
cascade performs go through `cascadeDeleteLibrary`, so they cross this boundary.
