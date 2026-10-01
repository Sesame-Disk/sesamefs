#!/usr/bin/env bash
# Directed mutations for ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01.
#
# Each mutation removes one part of the generation fence and must turn a
# focused test RED for its own reason. The stale-owner legs run against the
# real Cassandra of this checkout's dev stack (network <project>_default);
# the SERIAL pins are checked by the static Go guards. Mutations are applied to
# the source copied into the Docker image; the host tree is never modified.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

COMPOSE_PROJECT="$(docker compose -f docker-compose.yaml config 2>/dev/null | sed -n 's/^name: //p' | head -1)"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-sesamefs}"
NETWORK="${NONFENCING_CASSANDRA_NETWORK:-${COMPOSE_PROJECT}_default}"
RUNNER="$COMPOSE_PROJECT-library-hard-delete-lease-nonfencing-mutation-runner"
IMAGE="$COMPOSE_PROJECT-library-hard-delete-lease-nonfencing-mutation"
ENV_FILE="${ENV_FILE:-.env}"

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
fail() { red "FAILED: $*"; exit 1; }

cleanup() {
	local rc=$?
	docker rm -f "$RUNNER" >/dev/null 2>&1 || true
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

docker network inspect "$NETWORK" >/dev/null 2>&1 || fail "network $NETWORK not found; start this checkout's Cassandra (docker compose up -d cassandra)"
docker build -f Dockerfile.gotest -t "$IMAGE" . || fail "Docker Go test image build failed"
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
env_args=()
[ -f "$ENV_FILE" ] && env_args=(--env-file "$ENV_FILE")
docker run -d --name "$RUNNER" --network "$NETWORK" "${env_args[@]}" -e CASSANDRA_HOSTS=cassandra:9042 -e GOFLAGS=-buildvcs=false "$IMAGE" sleep 3600 >/dev/null ||
	fail "could not start Docker Go test runner"

INTEGRATION_TESTS='^(TestNonfencingT[0-9].*|TestNonfencingG[0-9].*|TestNonfencingR[0-9].*|TestNonfencingA[0-9].*|TestNonfencingGCHardDeleteLibraryIsGenerationFenced|TestNonfencingSoftDelete.*|TestNonfencingPermanentDelete.*)$'
STATIC_TESTS='^(TestLibraryLifecycleFencePinsGlobalSerial|TestPC0HeadSerialDomainPinsGlobalSerial)$'

run_tests() {
	local package="$1" regex="$2" tags="$3"
	docker exec "$RUNNER" go test $tags "$package" -count=1 -run "$regex" 2>&1
}

output="$(run_tests ./internal/api/v2 "$INTEGRATION_TESTS" "-tags integration")" || { printf '%s\n' "$output"; fail "stale-owner legs are not green before mutation"; }
output="$(run_tests ./internal/db "$STATIC_TESTS" "")" || { printf '%s\n' "$output"; fail "SERIAL pin guards are not green before mutation"; }
output="$(run_tests ./internal/gc '^TestWorker_Process(Library|Org)Cascade_.*RestoreAfterFence.*$' "")" || { printf '%s\n' "$output"; fail "GC worker legs are not green before mutation"; }

count=0
# mutate <name> <file> <perl substitution> <package> <test regex> <go test tags> <required failure text>
mutate() {
	local name="$1" file="$2" expr="$3" package="$4" test_regex="$5" tags="$6" reason="$7" backup output status
	backup="/tmp/$(basename "$file").orig"
	docker exec "$RUNNER" cp "$file" "$backup" || fail "could not back up $file"
	docker exec "$RUNNER" perl -0pi -e "$expr" "$file" || fail "could not apply mutation: $name"
	if docker exec "$RUNNER" cmp -s "$file" "$backup"; then
		fail "mutation did not change the source: $name"
	fi
	output="$(run_tests "$package" "$test_regex" "$tags")"
	status=$?
	docker exec "$RUNNER" cp "$backup" "$file" || fail "could not restore $file"
	if [ "$status" -eq 0 ]; then
		printf '%s\n' "$output"
		fail "mutation stayed green: $name"
	fi
	printf '%s\n' "$output" | grep -qF -- "$reason" || {
		printf '%s\n' "$output"
		fail "mutation failed for the wrong reason: $name"
	}
	green "RED as required: $name"
	count=$((count + 1))
}

LIFECYCLE=internal/db/library_lifecycle.go

# The delete and restore primitives first read the canonical state at SERIAL and
# return early on a changed generation; that read is not a fence (an owner can
# pause between it and the LWT), so the predicate mutations also drop it.
DELETE_GATE='if !state\.DeletedAt\.Equal\(deletedAt\) \{\n\t\t\treturn LibraryLifecycleGenerationChanged, nil\n\t\t\}\n'
RESTORE_GATES='if !state\.Present \{\n\t\t\treturn time\.Time\{\}, LibraryLifecycleTargetAbsent, nil\n\t\t\}\n\t\tif !state\.DeletedAt\.Equal\(deletedAt\) \{\n\t\t\treturn time\.Time\{\}, LibraryLifecycleGenerationChanged, nil\n\t\t\}\n'

mutate 'M1 canonical delete predicate removed (IF EXISTS)' "$LIFECYCLE" \
	's{(func DeleteTrashedLibraryGenerationWithIntent.*?)'"$DELETE_GATE"'(.*?DELETE FROM libraries WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \? AND lifecycle_at = \?(\s*`, orgID, libraryID), deletedAt, nullableTime\(state\.LifecycleAt\)\)}{$1$2IF EXISTS$3)}s' \
	./internal/api/v2 '^TestNonfencingT3StaleDeleteAfterRestore$' '-tags integration' \
	'stale permanent delete (err=<nil>) changed the library restored by the new owner'

mutate 'M2 canonical restore made an unconditional upsert' "$LIFECYCLE" \
	's{(func RestoreTrashedLibraryGenerationWithIntent.*?)'"$RESTORE_GATES"'(.*?WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \? AND lifecycle_at = \?(\s*`, restoredAt, restoredAt, orgID, libraryID), deletedAt, nullableTime\(state\.LifecycleAt\)\)}{$1$2$3)}s' \
	./internal/api/v2 '^TestNonfencingT4StaleRestoreBeforeDeleteCompletion$' '-tags integration' \
	'resurrected a library whose permanent delete had not completed yet'

mutate 'M3a canonical delete predicate accepts any trash generation' "$LIFECYCLE" \
	's{(func DeleteTrashedLibraryGenerationWithIntent.*?)'"$DELETE_GATE"'(.*?DELETE FROM libraries WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \? AND lifecycle_at = \?(\s*`, orgID, libraryID), deletedAt, nullableTime\(state\.LifecycleAt\)\)}{$1$2IF deleted_at != null$3)}s' \
	./internal/api/v2 '^TestNonfencingT5StaleDeleteAgainstNewerGeneration$' '-tags integration' \
	'NONFENCING RED: stale generation'

mutate 'M4b canonical delete fence downgraded to LOCAL_SERIAL' "$LIFECYCLE" \
	's{(IF deleted_at = \? AND lifecycle_at = \?\s*`, orgID, libraryID, deletedAt, nullableTime\(state\.LifecycleAt\)\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
	./internal/db '^(TestLibraryLifecycleFencePinsGlobalSerial|TestPC0HeadSerialDomainPinsGlobalSerial)$' '' \
	'library lifecycle fence no longer pins global SERIAL'

mutate 'M4c canonical restore fence downgraded to LOCAL_SERIAL' "$LIFECYCLE" \
	's{(`, restoredAt, restoredAt, orgID, libraryID, deletedAt, nullableTime\(state\.LifecycleAt\)\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
	./internal/db '^TestLibraryLifecycleFencePinsGlobalSerial$' '' \
	'library lifecycle fence no longer pins global SERIAL'

mutate 'M5 GC store completes after a changed generation' internal/gc/store_cassandra.go \
	's{\tif outcome == db\.LibraryLifecycleGenerationChanged \{\n.*?\n\t\treturn false, nil\n\t\}\n}{\t_ = outcome\n}s' \
	./internal/api/v2 '^TestNonfencingGCHardDeleteLibraryIsGenerationFenced$' '-tags integration' \
	'stale GC hard delete'

mutate 'M6 GC worker ignores a rejected hard delete' internal/gc/worker.go \
	's{\tif !deleted \{\n\t\treturn fmt\.Errorf\([^\n]*errLibraryCascadeGenerationChanged\)\n\t\}\n}{\t_ = deleted\n}' \
	./internal/gc '^TestWorker_ProcessOrgCascade_LibraryRestoreAfterFenceFailsClosed$' '' \
	'want errLibraryCascadeGenerationChanged'

mutate 'M7 GC no longer clears the marker a stopped restore left' internal/gc/store_cassandra.go \
	's{\t\tif err := db\.ClearRestoredLibraryMarker\([^\n]*\n\t\t\treturn false, err\n\t\t\}\n}{}' \
	./internal/api/v2 '^TestNonfencingGCHardDeleteLibraryIsGenerationFenced$' '-tags integration' \
	"GC left the restored library's stale marker behind"

WH=internal/api/v2/write_helpers.go
DH=internal/api/v2/library_delete_helpers.go

mutate 'M8 soft delete reverted to a plain client-timestamp write' internal/db/library_lifecycle.go \
	's{applied, err := session\.Query\(`\n\t\t\tUPDATE libraries SET deleted_at = \?, deleted_by = \?, updated_at = \?, lifecycle_at = \?\n\t\t\tWHERE org_id = \? AND library_id = \?\n\t\t\tIF deleted_at = null AND created_at != null AND lifecycle_at = \?\n\t\t`, deletedAt, deletedBy, deletedAt, deletedAt, orgID, libraryID, previousLifecycleAt\)\.\n\t\t\tSerialConsistency\(LibraryHeadSerialConsistency\)\.\n\t\t\tMapScanCAS\(previous\)}{_ = previousLifecycleAt\n\t\terr = session.Query(`UPDATE libraries USING TIMESTAMP ? SET deleted_at = ?, deleted_by = ?, updated_at = ?, lifecycle_at = ? WHERE org_id = ? AND library_id = ?`, now.UnixMicro(), deletedAt, deletedBy, deletedAt, deletedAt, orgID, libraryID).Exec()\n\t\tapplied := err == nil}' \
	./internal/api/v2 '^TestNonfencingSoftDeleteAfterRestoreWithClientClockBehind$' '-tags integration' \
	'soft delete after a restore lost with the client clock behind'

mutate 'M9 lifecycle clock not advanced past the previous generation' internal/db/library_lifecycle.go \
	's{if !previous\.IsZero\(\) && !next\.After\(previous\) \{}{if false \&\& !previous.IsZero() \&\& !next.After(previous) \{}' \
	./internal/api/v2 '^TestNonfencingG1SameMillisecondGenerations$' '-tags integration' \
	'is not after the first'

mutate 'M10 trashed lifecycle rows not stamped with the generation value' internal/db/library_lifecycle.go \
	's{\tbatch := session\.Batch\(gocql\.LoggedBatch\)\.WithTimestamp\(LibraryLifecycleWriteTimestamp\(at\)\)\n}{\tstamp := LibraryLifecycleWriteTimestamp(at)\n\tif !state.DeletedAt.IsZero() {\n\t\tstamp -= int64(2 * time.Hour / time.Microsecond)\n\t}\n\tbatch := session.Batch(gocql.LoggedBatch).WithTimestamp(stamp)\n}' \
	./internal/api/v2 '^TestNonfencingG2bSlowSoftDeleteAfterFastRestore$' '-tags integration' \
	'slow soft delete after a fast restore'

mutate 'M11 permanent delete cannot be resumed' "$DH" \
	's{(func resumeCommittedPermanentDelete\([^\n]*\n)}{$1\treturn trashLibraryCandidate{}, "", false, nil\n}' \
	./internal/api/v2 '^TestNonfencingPermanentDeleteCompletionFailureResumes$' '-tags integration' \
	'resume: resumed=false'

mutate 'M12 repair no longer rewrites the derived state' internal/db/library_lifecycle.go \
	's{(func RepairLibraryLifecycleDerivedState\([^\n]*\n)}{$1\treturn nil\n}' \
	./internal/api/v2 '^TestNonfencingSoftDeleteCompletionFailureRepairedOnRepeat$' '-tags integration' \
	'repair marker ='

mutate 'M15 permanent delete without its continuation marker' "$DH" \
	's{libraryID, orgID, deletedAt, storageClass, blockRepresentationID, dbpkg\.LibraryLifecycleWriteTimestamp\(deletedAt\)\)\.Exec\(\)}{uuid.NewString(), orgID, deletedAt, storageClass, blockRepresentationID, dbpkg.LibraryLifecycleWriteTimestamp(deletedAt)).Exec()}' \
	./internal/api/v2 '^TestNonfencingG7MissingMarkerPermanentDeleteResumes$' '-tags integration' \
	'repeated delete could not resume'

mutate 'M16 bulk clean does not resume committed deletes' internal/api/v2/org_admin_repos.go \
	's{resumed, resumeFailed := resumeCommittedPermanentDeletes\(h\.db, \[\]string\{targetOrgID\}\)}{resumed, resumeFailed := []resumedPermanentDelete(nil), 0}' \
	./internal/api/v2 '^TestNonfencingG6BulkCleanResumesCommittedDelete$' '-tags integration' \
	'repeated bulk clean left libraries_by_id'

mutate 'M17 repair does not rewrite the lifecycle-owned rows' internal/db/library_lifecycle.go \
	's{if err := writeLifecycleOwnedRows\(session, row, state, laterLifecycleValue\(state\), trashRows, resolveBlockRepresentation\); err != nil \{}{_ = trashRows\n\tif err := error(nil); err != nil \{}' \
	./internal/api/v2 '^TestNonfencingG8StaleMarkerRepairedToCurrentGeneration$' '-tags integration' \
	'repair after a stale marker'

mutate 'M18 GC soft-delete retry only checks the marker' internal/gc/store_cassandra.go \
	's{return db\.RepairLibraryLifecycleDerivedState\(session, orgID\.String\(\), libraryID\.String\(\), repair\)}{return nil}' \
	./internal/api/v2 '^TestNonfencingG9GCSoftDeleteRetryCompletesDerivedState$' '-tags integration' \
	'GC soft delete retry'

mutate 'M4d soft-delete fence downgraded to LOCAL_SERIAL' internal/db/library_lifecycle.go \
	's{(IF deleted_at = null AND created_at != null AND lifecycle_at = \?\s*`, deletedAt, deletedBy, deletedAt, deletedAt, orgID, libraryID, previousLifecycleAt\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
	./internal/db '^TestLibraryLifecycleFencePinsGlobalSerial$' '' \
	'library lifecycle fence no longer pins global SERIAL'

mutate 'M13 reaper drops a continuation whose transition can still apply' internal/db/library_lifecycle_pending.go \
	's{(func \(p LibraryLifecyclePending\) CanStillApply\(state LibraryLifecycleState\) bool \{\n)}{$1\treturn false\n}' \
	./internal/api/v2 '^TestNonfencingG4SoftDeleteDeathAccountingConverges$' '-tags integration' \
	'counters after recovery'

mutate 'M14 soft delete without a durable continuation' "$WH" \
	's{if err := dbpkg\.InsertLibraryLifecyclePending\(db\.Session\(\), intent\); err != nil \{}{if err := error(nil); err != nil \{}' \
	./internal/api/v2 '^TestNonfencingR5SoftDeleteDeathRecoveredByReaper$' '-tags integration' \
	'reaper after a soft delete died'

mutate 'M20 ordinary read-model rows stamped with the lifecycle clock' internal/db/library_lifecycle.go \
	's{\t\tbatch := session\.Batch\(gocql\.LoggedBatch\)\n\t\tif published != nil \{}{\t\tbatch := session.Batch(gocql.LoggedBatch).WithTimestamp(LibraryLifecycleWriteTimestamp(laterLifecycleValue(state)))\n\t\tif published != nil \{}' \
	./internal/api/v2 '^TestNonfencingR2LifecycleClockDoesNotPoisonOrdinaryWrites$' '-tags integration' \
	'old owner still lists the library'

mutate 'M21 permanent-delete resume reads its marker at session consistency' "$DH" \
	's{libraryID\)\.Consistency\(gocql\.EachQuorum\)\.Scan\(&markerOrgID}{libraryID).Scan(\&markerOrgID}' \
	./internal/api/v2 '^TestLibraryLifecycleRecoveryReadsAreStrong$' '' \
	'lifecycle recovery no longer reads at a strength that sees other datacenters'

mutate 'M22 repair reads the trash listing at session consistency' internal/db/library_lifecycle.go \
	's{trashRows, err := ListDeletedAdminLibraryRowsByOrgEachQuorum\(session, orgID\)}{trashRows, err := ListDeletedAdminLibraryRowsByOrg(session, orgID)}' \
	./internal/db '^TestLibraryLifecycleRepairReadsAreStrong$' '' \
	'lifecycle recovery no longer reads at a strength that sees other datacenters'

PENDING=internal/db/library_lifecycle_pending.go
REAPER=internal/api/v2/library_lifecycle_reaper.go

mutate 'M23 concurrent attempts share one continuation row' "$WH" \
	's#\}, uuid\.NewString\(\)\)#}, "00000000-0000-0000-0000-000000000000")#' \
	./internal/api/v2 '^TestNonfencingA1SameTargetLoserKeepsWinnerContinuation$' '-tags integration' \
	"the losing attempt removed the winner's continuation"

mutate 'M24 reaper never fences an abandoned attempt' "$REAPER" \
	's{if !pending\.Abandoned\(now\) \{}{if true \{}' \
	./internal/api/v2 '^TestNonfencingA2SoftDeleteAbandonedBeforeLWTIsFenced$' '-tags integration' \
	'abandoned soft-delete attempt still pending'

mutate 'M25 restore LWT ignores the lifecycle clock (fence ineffective)' "$LIFECYCLE" \
	's{IF deleted_at = \? AND lifecycle_at = \?(\s*`, restoredAt, restoredAt, orgID, libraryID, deletedAt), nullableTime\(state\.LifecycleAt\)\)}{IF deleted_at = ?$1)}' \
	./internal/api/v2 '^TestNonfencingA3RestoreFencedWhileParkedCannotCommitUntracked$' '-tags integration' \
	'the fenced attempt committed without a continuation'

mutate 'M26 delete LWT ignores the lifecycle clock (fence ineffective)' "$LIFECYCLE" \
	's{IF deleted_at = \? AND lifecycle_at = \?(\s*`, orgID, libraryID, deletedAt), nullableTime\(state\.LifecycleAt\)\)}{IF deleted_at = ?$1)}' \
	./internal/api/v2 '^TestNonfencingA4PermanentDeleteFencedWhileParkedCannotCommitUntracked$' '-tags integration' \
	'the fenced attempt committed without a continuation'

mutate 'M27 read-model publication not confirmed (restore)' "$LIFECYCLE" \
	's{if libraryProjectionCurrent\(row, state, next, nextState\) \{}{if true \{}' \
	./internal/api/v2 '^TestNonfencingA6RestorePublicationConfirmsItsSnapshot$' '-tags integration' \
	'restore published a stale snapshot over a transfer'

mutate 'M28 read-model publication not confirmed (repair)' "$LIFECYCLE" \
	's{if libraryProjectionCurrent\(row, state, next, nextState\) \{}{if true \{}' \
	./internal/api/v2 '^TestNonfencingA7RepairPublicationConfirmsItsSnapshot$' '-tags integration' \
	'repair published a stale snapshot over a transfer'

mutate 'M29 bulk cleanup ignores the continuations' "$DH" \
	's{pending, err := listPendingPermanentDeletesFn\(database, wanted\)}{pending, err := [][2]string(nil), error(nil)}' \
	./internal/api/v2 '^TestNonfencingA8BulkCleanDiscoversDeleteThroughContinuation$' '-tags integration' \
	'bulk discovery missed a committed permanent delete'

mutate 'M30 continuations discovered at session consistency' "$PENDING" \
	's{(FROM library_lifecycle_pending WHERE recovery_bucket = \?\n\t`, bucket\))\.Consistency\(LibraryLifecyclePendingConsistency\)}{$1}' \
	./internal/db '^TestLibraryLifecyclePendingIsGlobalQuorum$' '' \
	'library lifecycle continuation no longer read/written at global QUORUM'

mutate 'M31 continuation retired at an implicit client timestamp' "$PENDING" \
	's{DELETE FROM library_lifecycle_pending USING TIMESTAMP \?\n(.*?)`, libraryLifecyclePendingRetireTimestamp\(p, time\.Now\(\)\), }{DELETE FROM library_lifecycle_pending\n$1`, }s' \
	./internal/api/v2 '^TestNonfencingA11PendingRetirementBeatsFastInsert$' '-tags integration' \
	'retired continuation still visible'

mutate 'M32 projected deleted_at written with the client clock' "$LIFECYCLE" \
	's{(\t\t\tAddUpsertAdminLibraryOrdinaryRowsQuery\(batch, row\)\n)}{$1\t\t\tif row.DeletedAt != nil {\n\t\t\t\tbatch.Query(`UPDATE libraries_by_owner SET deleted_at = ? WHERE org_id = ? AND owner_id = ? AND library_id = ?`, *row.DeletedAt, row.OrgID, row.OwnerID, row.LibraryID)\n\t\t\t\tbatch.Query(`UPDATE libraries_by_org_updated SET deleted_at = ? WHERE org_id = ? AND library_id = ?`, *row.DeletedAt, row.OrgID, row.LibraryID)\n\t\t\t} else {\n\t\t\t\tbatch.Query(`DELETE deleted_at FROM libraries_by_owner WHERE org_id = ? AND owner_id = ? AND library_id = ?`, row.OrgID, row.OwnerID, row.LibraryID)\n\t\t\t\tbatch.Query(`DELETE deleted_at FROM libraries_by_org_updated WHERE org_id = ? AND library_id = ?`, row.OrgID, row.LibraryID)\n\t\t\t}\n}; s{if cellAt := projectedLifecycleValue\(state\); !cellAt\.IsZero\(\) \{}{if cellAt := projectedLifecycleValue(state); false \&\& !cellAt.IsZero() \{}' \
	./internal/api/v2 '^TestNonfencingA9ProjectedDeletedAtFollowsRestoreAfterFastSoftDelete$' '-tags integration' \
	'restored library still projected as deleted'

mutate 'M33 read-model publication confirmed at SERIAL only' "$LIFECYCLE" \
	's{(`, row\.OrgID, row\.LibraryID\))\.Consistency\(gocql\.EachQuorum\)(\.Scan\(\n\t\t&ordinary\.OwnerID)}{$1$2}' \
	./internal/db '^TestLibraryLifecycleRound6Pins$' '' \
	'does not read at EACH_QUORUM'

mutate 'M34 trash reconciliation deletes on a weak canonical read' internal/db/admin_library_read_models.go \
	's{\t\tstate, err := ReadLibraryLifecycleSerial\(session, row\.OrgID, row\.LibraryID\)\n\t\tif err != nil \{\n\t\t\treturn nil, cleaned, err\n\t\t\}\n\t\tif state\.Present && state\.DeletedAt\.Equal\(row\.DeletedAt\) \{\n\t\t\tkept = append\(kept, row\)\n\t\t\tcontinue\n\t\t\}\n}{}' \
	./internal/db '^TestLibraryLifecycleRound6Pins$' '' \
	'deletes without the lifecycle authority'

mutate 'M35 permanent-delete completion stamped before its LWT' "$DH" \
	's{\t\tbatch := database\.Session\(\)\.Batch\(gocql\.LoggedBatch\)\n(\t\tif err := addPermanentDeleteCompletionQueries\()}{\t\tbatch := database.Session().Batch(gocql.LoggedBatch).WithTimestamp(permanentDeleteCompletionTimestamp(deletedAt, deletedAt))\n$1}; s{\t\tbatch\.WithTimestamp\(permanentDeleteCompletionTimestamp\(deletedAt, lifecycleFloor\)\)\n}{\t\t_ = lifecycleFloor\n}' \
	./internal/api/v2 '^TestNonfencingA12PermanentDeleteCompletionAfterFenceRemovesRepairedRows$' '-tags integration' \
	'survived the permanent delete completed after a fence'

mutate 'M37 permanent-delete completion ignores the winning lifecycle floor' "$DH" \
	's{\tif lifecycleFloor\.Before\(deletedAt\) \{}{\tif true \{}' \
	./internal/api/v2 '^TestNonfencingA14PermanentDeleteCompletionUsesWinningLifecycleFloor$' '-tags integration' \
	'the completion lost to the rows repaired at a fence value ahead of real time'

mutate 'M36 resumed permanent delete does not recover the lifecycle floor' "$DH" \
	's{(func permanentDeleteLifecycleFloor\([^\n]*\n)}{$1\treturn time.Time{}, nil\n}' \
	./internal/api/v2 '^TestNonfencingA13ResumedPermanentDeleteRecoversLifecycleFloor$' '-tags integration' \
	'the resumed completion lost to the rows repaired at the fence value'

green "All $count NONFENCING mutations went RED for their own reason."
