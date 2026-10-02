# Library trash lifecycle contract

**Status:** normative. Closes `ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01`.
**Supersedes:** the lifecycle redesign proposed in PR #240 (closed unmerged).
The canonical generation CAS is retained. Completion now also handles interrupted
operations and delayed publication of derived state.

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
| Soft delete (`softDeleteLibrary`, `CassandraStore.SoftDeleteLibrary`) | `UPDATE … SET deleted_at = D … IF deleted_at = null AND created_at != null` (`SoftDeleteCanonicalLibraryGeneration`) | already trashed → idempotent (it repairs the current generation on retry), or the row is absent |
| Restore (`restoreDeletedLibrary`) | `UPDATE … SET deleted_at = null, deleted_by = null, updated_at = ? … IF deleted_at = D` (`ClearCanonicalLibraryGeneration`) | the row was purged or is at another generation → `library is pending permanent deletion` |
| GC cascade (`CassandraStore.HardDeleteLibrary`) | `DELETE FROM libraries … IF deleted_at = D` (`DeleteCanonicalLibraryAtGeneration`) | the row exists under another generation → the item finishes as stale |
| API permanent delete (`hardDeleteLibraryRowsFn`) | same statement | the row exists under another generation, or absent without a valid durable purge intent → `409 library is no longer in trash` |

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

`deleted_libraries` is discovery and recovery metadata, never canonical destruction
authority. Marker creation, generation replacement and generation-scoped deletion
use global-SERIAL LWTs. An insert cannot lose to a previous LWT tombstone because
of a client clock behind Cassandra. Lifecycle marker writes use TTL 0. Marker replacement observes its expected
marker before checking canonical state, then conditions the LWT on that identity.
ACTIVE completion removes only the marker captured at entry; it cannot consume a
newer generation's durable purge intent.

- **Soft delete:** canonical CAS, then `CompleteLibraryTrashLifecycle`. A repeated
  authenticated owner DELETE may read a trashed canonical row and finish completion.
  An existing marker is not sufficient to skip indices or accounting.
- **Restore:** retain/create the current generation marker before the canonical CAS.
  Complete derived state and accounting before removing the recovery marker. A
  present ACTIVE row with an outstanding marker is an interrupted-completion retry;
  an ordinary ACTIVE row without a marker still rejects restore. Both user and org
  admin restore endpoints support this retry under their existing authorization.
- **API permanent delete:** before the canonical CAS, persist `purge_requested_at`,
  owner, creation time, org, storage class and representation on the generation-bound
  marker. Migration 028 adds the owner and creation-time keys. If the row disappears
  after a committed CAS, the same authorized HTTP DELETE can finish exact index,
  lookup and policy cleanup using this intent with GC disabled. A live canonical
  row always controls whether the delete is stale; the intent cannot override it.
- **GC hard delete:** canonical generation CAS, idempotent derived cleanup, then
  conditional marker deletion. Its marker deletion shares the marker LWT domain.

`RepairLibraryTrashDerivedState` reads canonical state through SERIAL, publishes
its current marker and indices, then reads canonical state again. If the generation
changed during publication, it repairs the newer state. Canonical absence removes
old projections and never upserts `libraries`. It removes obsolete trash projection
keys for the library, including D1 when canonical state is D2. The read-side trash
reconciler also checks the exact generation, rather than keeping any trashed row.
Four unsuccessful repair rounds return an error rather than success.

Accounting completion reconciles the organization, owner and platform shard from
canonical live `size_bytes`/`file_count`. It checks both canonical totals and physical
counter values after applying the correction. This avoids replaying an old +storage
or a clamped -storage after D2 or an ambiguous counter result. Eight bounded rounds
with contention backoff return an error if usage does not settle; the existing
`gc_storage_counter_reconciliation` requests remain durable. This cold path scans
canonical library metadata; its cost grows with the number of libraries. It adds no
SERIAL reads to upload/dedup paths and does not serialize ordinary size writers.

An ambiguous restore CAS is attributed only after a successful lease fence, a
SERIAL observation of a present ACTIVE canonical row, and a **second** successful
lease fence after that observation. A different restorer's clear is not evidence of
ownership by the original request.

## 4. Crash and ambiguity analysis

| Interruption | Durable state | Recovery |
|---|---|---|
| Soft delete after CAS, before completion | canonical D; marker/indices may be absent | Authenticated owner DELETE repeats the canonical conditional operation and repairs current derived state and usage. No GC service is required. |
| Old soft-delete/restore publishes after D2 | canonical D2; derived snapshot may temporarily be stale | Post-publication SERIAL validation repairs D2. If the process stops before validation, owner DELETE of D2 remains reachable and repairs its projections/marker/usage. |
| Restore after CAS, before completion | ACTIVE canonical plus retained marker | Authorized user/org restore retry completes indices and accounting before removing the marker. If GC is enabled, its existing stale-generation handling and aggregate reconciliation requests also remain available. |
| Restore accounting fails | canonical ACTIVE/D2; reconciliation request; ACTIVE recovery marker retained | An error is returned. The authorized retry reconciles canonical totals rather than replaying a signed obligation. |
| Restore CAS has an unknown result | generation outcome uncertain | Two lease fences surrounding the SERIAL observation are required to attribute success; otherwise return an error. |
| API permanent delete after CAS, before batch | canonical absent; durable purge intent with exact keys | The original owner, same-org admin or platform superadmin can retry the HTTP endpoint with GC off. Repeated cleanup is idempotent. |
| API permanent delete CAS is ambiguous | durable intent; canonical may be present or absent | Retry under the lease; canonical generation still gates deletion. |
| GC after canonical CAS, before batch | canonical absent; generation marker remains | Existing cascade retry completes derived cleanup, then conditionally deletes its marker. |

These are explicit retry contracts, not an automatic repair service when GC is off.
Neither marker writes nor ordinary projection batches are transactional with the
canonical partition. Persistent contention may require another request.

## 5. Known residuals (not changed here)

- Ordinary owner transfer, rename and size writers remain plain upserts. Their
  ghost-cell behavior after a hard delete is tracked separately in
  `ISSUE-PCD1B-CONTINUITY-LWT-GHOST-ROW-01` and related follow-ups.
- Link cleanup still precedes the permanent-delete CAS. A losing request can remove
  links; `ISSUE-LIBRARY-PERMANENT-DELETE-LINK-CLEANUP-BEFORE-CAS-01` remains open.
- Single-node Docker Cassandra validates these completion interleavings, not network
  partitions or a multi-DC deployment. Global SERIAL remains the declared domain.

## 6. Evidence

The original boundary regressions and mutation evidence are listed below. Completion
regressions and their final validation are recorded in [LIBRARY-TRASH-COMPLETION-AUDIT.md](./LIBRARY-TRASH-COMPLETION-AUDIT.md).

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
