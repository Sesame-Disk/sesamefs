# Library trash completion audit

Audit date: 2026-10-01. Original reviewed head: `b15d3f8fb246b00040a55972a4b77382004bd87b`,
base `3f0b787a23bc407fca2df09c0a534b19f15b57b0`, PR #243.
The six issues in the supplied crossed audit are supported. The canonical
`libraries.deleted_at` generation CAS remains the lifecycle boundary.

## Confirmed findings and corrections

| Finding | Correction and regression |
|---|---|
| P1: permanent-delete CAS commits, then completion is unreachable without GC | Persist a TTL-0 generation-bound purge intent before CAS. Migration 028 retains owner and creation time for authenticated retries and exact index keys. `PermanentDeleteCrashIsReachableByHTTPRetryWithoutGC` interrupts between CAS and batch, then calls the normal handler twice. |
| P1: paused D1 restore followed by D2 undercounts other live storage | Reconcile canonical live usage synchronously and validate the resulting counters; retain bounded retries and durable reconciliation requests. `RestoreOwesStorageEvenAfterD2` covers zero and nonzero other live usage; `ConcurrentRestoresKeepSharedCounters` covers two libraries in one org/user scope. |
| P2: interrupted soft-delete completion cannot be retried through owner DELETE | Accept a trashed canonical row on this authenticated owner route and repair completion even if its marker already matches. `SoftDeleteCrashIsReachableByHTTPRetry`; owner authorization unit regression. |
| P2: stale restore publication overwrites D2 and leaves D1/D2 trash rows | SERIAL validation after publishing, bounded repair of changed generations/absence, and deletion of obsolete trash keys. `RestoreRevalidatesAfterPublishing`, `RestoreRemovesPreviousTrashProjection`, `RestoreRepairsIndexesAfterConcurrentPurge`. Assertions inspect raw projection keys before read-side reconciliation. |
| P2: an ordinary marker insert is shadowed by a previous LWT tombstone | Marker inserts/replacements/deletes all use global SERIAL LWTs. `MarkerInsertFollowsLWTTombstoneDespiteClientSkew` first demonstrates the old insert disappearing, then production completion installs D2. |
| P2: lease lost during SERIAL observation can misattribute ambiguous restore | Fence both before and after the observation. `TestSettleAmbiguousLibraryGenerationClear_LosesLeaseDuringObservation`. |

Independent audit findings about a delayed soft-delete republishing D1 over ACTIVE
or D2 are covered by `PausedSoftDeleteDoesNotHideRestoredLibrary` and
`PausedSoftDeletePreservesNewerMarker`. The duplicate trash-row finding overlaps the
fourth report item and is not counted twice.

Two further supported completion issues were corrected: ACTIVE restore completion
retains its recovery marker until indices and accounting finish
(`RestoreCrashIsReachableByHTTPRetryWithoutGC`), and permanent delete reconciles
usage even if the preceding trash completion stopped before accounting
(`PermanentDeleteAfterInterruptedTrashRepairsStorage`). Retries retain owner/org
checks (`PermanentDeleteRetryRetainsAuthorization`).

Two final adversarial interleavings protect the newer durable purge intent from
old restore marker cleanup and old trash publication:
`StaleRestoreCannotDeleteNewerPurgeIntent` and
`PausedTrashPublicationPreservesNewerPurgeIntent`. Marker cleanup names the identity
captured at completion entry. Marker publication reads its expected marker identity
before validating canonical state, then uses that identity in its LWT.

The HTTP tests also exposed a pre-existing permanent-delete tag cleanup failure:
Cassandra rejects a batch mixing `repo_tag_file_counts` (COUNTER) and ordinary tag
tables. Delete the counter partition separately before deleting its discovery
sources; `PermanentDeleteCleansTagCounters` verifies cleanup twice against Cassandra.

## Validation

All Go builds and tests ran in Docker (Go 1.25.14). Cassandra 5.0.9 used a private
keyspace on a private single-node container, `datacenter1`, RF=1. The HTTP backend
was built from the corrected source and used this keyspace with `GC_ENABLED=false`.
The default user stack was not rebuilt or migrated.

- `go test ./... -short -count=1 -timeout 10m`: PASS for all packages. The clean
  container source snapshot excludes unrelated local `tmp/` files; it contains all
  correction files, including new files. Repository authority/inventory guards ran.
- `go test -race ./internal/gc ./internal/traffic ./internal/api/v2 -short -count=1`:
  PASS.
- `go test -tags integration -race ./internal/api/v2 -run
  'TestTrashCompletion|TestRestoreDeletedLibrary|TestPermanentDelete|TestSoftDeleteThenImmediateRestore|TestCassandraHardDeleteLibrary'
  -count=10 -timeout 10m`: PASS. Includes 16 new completion regressions, original
  boundary regressions and permanent-delete unit regressions.
- Targeted `internal/integration` HTTP/projection/reconciliation/lease tests:
  10 PASS, 1 SKIP (`TestGC_LibraryCascade`: server GC disabled), 0 FAIL.
- Mutation copies, separate from the tested source: removing the second lease
  fence, skipping the durable permanent-delete intent, omitting publication
  revalidation, replacing the captured marker with the latest generation at cleanup,
  and restoring the mixed counter/ordinary tag batch each make the
  corresponding regression FAIL.
- `git diff --check`: PASS. Migration 028 applied successfully on the isolated
  keyspace; existing migrations were not edited.

## Practical limits

Migration 028 must run before the corrected runtime uses its marker columns.
Completion uses existing marker and reconciliation tables, with no lifecycle clock,
new reaper or changes to user/org transitions. Canonical absence is never restored
from a projection or an intent. Purging file content remains GC work; HTTP retries
with GC off complete metadata and accounting, not S3 reclamation.

Reconciliation scans canonical library metadata on this cold path and therefore
costs O(number of libraries) per snapshot. It checks running totals, uses at most
eight contention rounds, and returns an error if it cannot settle. Ordinary upload,
rename and owner-transfer concurrency is not converted into a transaction here.
Recovery with GC off requires an authorized retry; it is not an autonomous service.
Single-node tests do not establish partition or multi-DC availability guarantees.
The pre-existing link-cleanup-before-CAS and ordinary ghost-writer issues remain
tracked separately in the lifecycle contract.
