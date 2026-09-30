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

INTEGRATION_TESTS='^(TestNonfencingT[0-9].*|TestNonfencingGCHardDeleteLibraryIsGenerationFenced|TestNonfencingSoftDelete.*|TestNonfencingPermanentDelete.*)$'
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

mutate 'M1 canonical delete predicate removed (IF EXISTS)' "$LIFECYCLE" \
	's{(DELETE FROM libraries WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \?(\s*`, orgID, libraryID), deletedAt\)}{$1IF EXISTS$2)}' \
	./internal/api/v2 '^TestNonfencingT3StaleDeleteAfterRestore$' '-tags integration' \
	'stale permanent delete (err=<nil>) changed the library restored by the new owner'

mutate 'M2 canonical restore made an unconditional upsert' "$LIFECYCLE" \
	's{(WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \?(\s*`, updatedAt, orgID, libraryID), deletedAt\)}{$1$2)}' \
	./internal/api/v2 '^TestNonfencingT4StaleRestoreBeforeDeleteCompletion$' '-tags integration' \
	'resurrected a library whose permanent delete had not completed yet'

mutate 'M3a canonical delete predicate accepts any trash generation' "$LIFECYCLE" \
	's{(DELETE FROM libraries WHERE org_id = \? AND library_id = \?\s*)IF deleted_at = \?(\s*`, orgID, libraryID), deletedAt\)}{$1IF deleted_at != null$2)}' \
	./internal/api/v2 '^TestNonfencingT5StaleDeleteAgainstNewerGeneration$' '-tags integration' \
	'NONFENCING RED: stale generation'

mutate 'M4b canonical delete fence downgraded to LOCAL_SERIAL' "$LIFECYCLE" \
	's{(IF deleted_at = \?\s*`, orgID, libraryID, deletedAt\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
	./internal/db '^(TestLibraryLifecycleFencePinsGlobalSerial|TestPC0HeadSerialDomainPinsGlobalSerial)$' '' \
	'library lifecycle fence no longer pins global SERIAL'

mutate 'M4c canonical restore fence downgraded to LOCAL_SERIAL' "$LIFECYCLE" \
	's{(`, updatedAt, orgID, libraryID, deletedAt\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
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
	's{\t\tif err := db\.ClearStaleSoftDeleteMarker\([^\n]*\n\t\t\treturn false, err\n\t\t\}\n}{}' \
	./internal/api/v2 '^TestNonfencingGCHardDeleteLibraryIsGenerationFenced$' '-tags integration' \
	"GC left the restored library's stale marker behind"

WH=internal/api/v2/write_helpers.go
DH=internal/api/v2/library_delete_helpers.go

mutate 'M8 soft delete reverted to a plain client-timestamp write' "$WH" \
	's{outcome, err := dbpkg\.SoftDeleteLibraryGeneration\(db\.Session\(\), orgID, libraryID, now, deletedBy\)}{outcome, err := dbpkg.LibraryLifecycleApplied, db.Session().Query(`UPDATE libraries USING TIMESTAMP ? SET deleted_at = ?, deleted_by = ?, updated_at = ? WHERE org_id = ? AND library_id = ?`, now.UnixMicro(), now, deletedBy, now, orgID, libraryID).Exec()}' \
	./internal/api/v2 '^TestNonfencingSoftDeleteAfterRestoreWithClientClockBehind$' '-tags integration' \
	'soft delete after a restore lost with the client clock behind'

mutate 'M9 completion stamp without the read-model floor' internal/db/library_lifecycle.go \
	's{(func LibraryLifecycleCompletionStamp\([^\n]*\n\tstamp := now\.UnixMicro\(\)\n)}{$1\treturn stamp\n}' \
	./internal/api/v2 '^TestNonfencingSoftDeleteAfterRestoreWithClientClockBehind$' '-tags integration' \
	"soft delete marker lost to the restore's marker removal"

mutate 'M10 restore completion stamped after its transition' "$WH" \
	's{batch := db\.Session\(\)\.Batch\(gocql\.LoggedBatch\)\.WithTimestamp\(stamp\)\n\tbatch\.Query\(`DELETE FROM deleted_libraries WHERE library_id = \?`, libraryID\)}{batch := db.Session().Batch(gocql.LoggedBatch).WithTimestamp(stamp - stamp + time.Now().UnixMicro())\n\tbatch.Query(`DELETE FROM deleted_libraries WHERE library_id = ?`, libraryID)}' \
	./internal/api/v2 '^TestNonfencingT5StaleRestoreCompletionAgainstNewerGeneration$' '-tags integration' \
	'stale restore completion (err=<nil>) removed the generation'

mutate 'M11 permanent delete cannot be resumed' "$DH" \
	's{(func resumeCommittedPermanentDelete\([^\n]*\n)}{$1\treturn trashLibraryCandidate{}, "", false, nil\n}' \
	./internal/api/v2 '^TestNonfencingPermanentDeleteCompletionFailureResumes$' '-tags integration' \
	'resume: resumed=false'

mutate 'M12 repeated delete no longer repairs a trashed library' "$WH" \
	's{(func repairTrashedLibraryDerivedState\([^\n]*\n)}{$1\treturn nil\n}' \
	./internal/api/v2 '^TestNonfencingSoftDeleteCompletionFailureRepairedOnRepeat$' '-tags integration' \
	'repair marker ='

mutate 'M4d soft-delete fence downgraded to LOCAL_SERIAL' internal/db/library_lifecycle.go \
	's{(IF deleted_at = null AND created_at != null\s*`, deletedAt, deletedBy, deletedAt, orgID, libraryID\)\.\s*)SerialConsistency\(LibraryHeadSerialConsistency\)}{$1SerialConsistency(gocql.LocalSerial)}' \
	./internal/db '^TestLibraryLifecycleFencePinsGlobalSerial$' '' \
	'library lifecycle fence no longer pins global SERIAL'

green "All $count NONFENCING mutations went RED for their own reason."
