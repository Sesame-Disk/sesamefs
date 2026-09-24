#!/usr/bin/env bash
# PC-D1B.4 guard mutations. Each edit below must turn a PC-D1B.4 guard RED for
# its specific reason: the lifecycle/destroyer inventory, the fence-column
# guard, the fresh-library-id guard, the alias guard, the model's selected-fence
# generation-ownership and stale-generation proofs and, with
# --with-cassandra, the real-Cassandra characterization. Files are restored
# after every leg. All Go runs happen inside Docker.
set -uo pipefail
cd "$(dirname "$0")/.."

TEST_IMAGE=${PCD1B4_MUTATION_IMAGE:-sesamefs-pcd1b4-gotest}
RUNNER="sesamefs-pcd1b4-mutation-runner-$$"
WITH_CASSANDRA=0
CASSANDRA_NETWORK=${PCD1B4_CASSANDRA_NETWORK:-sesamefs-dev-wsl_default}
BACKUPS=()

for arg in "$@"; do
    case "$arg" in
        --with-cassandra) WITH_CASSANDRA=1 ;;
        *) echo "usage: $0 [--with-cassandra]" >&2; exit 2 ;;
    esac
done

restore() {
    local entry
    for entry in "${BACKUPS[@]}"; do
        mv -f "${entry#*|}" "${entry%%|*}"
    done
    BACKUPS=()
}

fail() { echo "FAILED: $*" >&2; restore; exit 1; }

cleanup() {
    restore
    docker rm -f "$RUNNER" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

mutate() {
    local target="$1" expr="$2" backup
    backup="$target.pcd1b4bak.$$"
    cp "$target" "$backup"
    BACKUPS+=("$target|$backup")
    perl -0pi -e "$expr" "$target"
    cmp -s "$target" "$backup" && fail "mutation of $target did not apply"
}

append() {
    local target="$1" text="$2" backup
    backup="$target.pcd1b4bak.$$"
    cp "$target" "$backup"
    BACKUPS+=("$target|$backup")
    printf '%s\n' "$text" >> "$target"
}

expect_red() {
    local label="$1" diagnostic="$2" pattern="$3" package="${4:-./internal/db}" tags="${5:-}" out status
    out="$(docker exec "$RUNNER" go test $tags "$package" -count=1 -run "$pattern" 2>&1)"
    status=$?
    restore
    if [ "$status" -eq 0 ]; then
        echo "$out"
        fail "$label stayed green"
    fi
    if [[ "$out" != *"$diagnostic"* ]]; then
        echo "$out"
        fail "$label did not trip its targeted assertion: $diagnostic"
    fi
    echo "RED as required: $label"
}

# A branch-local image with modules already downloaded; the source is bind
# mounted over /build so every mutation is what the tests see.
if ! docker image inspect "$TEST_IMAGE" >/dev/null 2>&1; then
    docker build -f Dockerfile.gotest -t "$TEST_IMAGE" . || fail "build $TEST_IMAGE"
fi

network_args=()
if [ "$WITH_CASSANDRA" -eq 1 ]; then
    # Reuses a running dev stack (network, Cassandra and dev-token backend) the
    # same way its go-integration-test service does; nothing in it is modified.
    network_args=(--network "$CASSANDRA_NETWORK" -e CASSANDRA_HOSTS=cassandra:9042 -e SESAMEFS_URL=http://sesamefs:8080)
    if [ -f "${ENV_FILE:-.env}" ]; then
        network_args+=(--env-file "${ENV_FILE:-.env}")
    fi
fi
docker run -d --name "$RUNNER" "${network_args[@]}" -v "$PWD":/build -w /build "$TEST_IMAGE" sleep 3600 >/dev/null

echo "==> baseline: every PC-D1B.4 guard is green"
docker exec "$RUNNER" go test ./internal/db -count=1 -run 'PCD1B4' >/dev/null || fail "baseline PC-D1B.4 guards are not green"

# G1: a new soft-delete statement outside the inventory.
append internal/gc/store_cassandra.go '
func pcd1b4MutationSoftDelete(session *gocql.Session) error {
	return session.Query(`UPDATE libraries SET deleted_at = ? WHERE org_id = ? AND library_id = ?`).Exec()
}'
expect_red "G1 unlisted soft-delete" "unlisted soft-delete at internal/gc/store_cassandra.go:pcd1b4MutationSoftDelete" '^TestPCD1B4LifecycleStatementsAreInventoried$'

# G2: a new destroyer call site outside the inventory (CW-M7 precursor).
append internal/api/v2/publish_repair.go '
func pcd1b4MutationDestroy(database *db.DB, repoID, fsID string) error {
	return db.DeleteFSObjectIdentity(database.Session(), repoID, fsID)
}'
expect_red "G2 unlisted destroyer" "unlisted DeleteFSObjectIdentity call at internal/api/v2/publish_repair.go:pcd1b4MutationDestroy" '^TestPCD1B4DestroyerCallSitesAreInventoried$'

# G3: removing restore's canonical DELETE deleted_at leaves the inventory stale.
mutate internal/api/v2/write_helpers.go 's/DELETE deleted_at, deleted_by FROM libraries/DELETE deleted_by FROM libraries/'
expect_red "G3 inventoried restore disappears" "inventoried restore no longer found at internal/api/v2/write_helpers.go:restoreDeletedLibrary" '^TestPCD1B4LifecycleStatementsAreInventoried$'

# G4: a fence column written in production before PC-D1B.5 (CW-M9 precursor).
append internal/db/library_continuity.go '
const pcd1b4MutationFenceWrite = `UPDATE libraries SET continuity_destruction_epoch = now() WHERE org_id = ? AND library_id = ?`'
expect_red "G4 premature fence write" "fence columns appear in production code" '^TestPCD1B4FenceColumnsAreNotYetWritten$'

# G5: a library creator binding a non-fresh id.
mutate internal/api/v2/org_admin_groups.go 's/newLibID := uuid\.New\(\)\.String\(\)/newLibID := c.Param("group_id")/'
expect_red "G5 reused library id" "library creators whose library_id is not a freshly minted UUID" '^TestPCD1B4LibraryCreatorsMintFreshIDs$'

# G6: the selected fence without its epoch predicate must lose its safety proof.
mutate internal/db/pcd1b4_certification_window_model_test.go 's/captures: true, captureRequiresIdle: true, casEpochPredicate: true\}\n\)/captures: true, captureRequiresIdle: true}\n)/'
expect_red "G6 model selected fence weakened" "PC-D1B.4 MODEL: selected fence violated" '^TestPCD1B4ModelSelectedFenceHoldsInvariants$'

# G7: the model must still see the current runtime as unsafe; a model that
# silently fences destroyers for the current runtime is vacuous.
mutate internal/db/pcd1b4_certification_window_model_test.go 's/cwCurrentRuntime = cwDesign\{name: "current runtime[^"]*"\}/cwCurrentRuntime = cwSelected/'
expect_red "G7 model current-runtime drift" "the current runtime no longer admits a witness born after an in-window delete" '^TestPCD1B4ModelCurrentRuntimeAdmitsFalseWitness$'

# G8: an alias of a destroyer primitive escapes call-site inventory.
append internal/gc/worker.go '
var pcd1b4MutationDeleteIdentity = db.DeleteFSObjectIdentity'
expect_red "G8 aliased destroyer primitive" "DeleteFSObjectIdentity referenced without a call at internal/gc/worker.go:pcd1b4MutationDeleteIdentity" '^TestPCD1B4DestroyerPrimitivesAreNotAliased$'

# G9: a new wrapper around a destroyer primitive is a new unlisted call site.
append internal/api/v2/fs_helpers.go '
func pcd1b4MutationWrapper(database *db.DB, repoID, commitID string) error {
	return db.DeleteCommitIdentity(database.Session(), repoID, commitID)
}'
expect_red "G9 new destroyer wrapper" "unlisted DeleteCommitIdentity call at internal/api/v2/fs_helpers.go:pcd1b4MutationWrapper" '^TestPCD1B4DestroyerCallSitesAreInventoried$'

# G10: a raw block_references delete bypasses RemoveBlockReference.
append internal/gc/store_cassandra.go '
func pcd1b4MutationRawReferenceDelete(session *gocql.Session) error {
	return session.Query(`DELETE FROM block_references WHERE org_id = ? AND block_id = ? AND referrer = ?`).Exec()
}'
expect_red "G10 raw reference delete" "unlisted reference-delete at internal/gc/store_cassandra.go:pcd1b4MutationRawReferenceDelete" '^TestPCD1B4LifecycleStatementsAreInventoried$'

# G11: the model must catch a completion that ignores its generation.
mutate internal/db/pcd1b4_certification_window_model_test.go 's/\tif d\.pendingByGeneration && n\.pendingGen\[token\] != gen \{\n\t\treturn\n\t\}\n//'
expect_red "G11 generation-blind completion" "PC-D1B.4 MODEL: selected fence violated in retry/same-token-stale-completion" '^TestPCD1B4ModelSelectedFenceHoldsInvariants$'

# G12: without generation-timestamped tombstones a paused generation's late
# delete breaks a certified witness.
mutate internal/db/pcd1b4_certification_window_model_test.go 's/\t\ttombstoneAtGeneration: true, recordsSuperseded: true, certifierReaffirms: true,/\t\trecordsSuperseded: true, certifierReaffirms: true,/'
expect_red "G12 stale generation writes with a current timestamp" "PC-D1B.4 MODEL: audit stale destructive write under" '^TestPCD1B4ModelStaleGenerationCannotDestroyLate$'

if [ "$WITH_CASSANDRA" -eq 1 ]; then
    # C1: a witness CAS without the deleted_at predicate changes R1 on real Cassandra.
    mutate internal/db/library_continuity.go 's/(func CommitLibraryContinuityWitnessContext.*?IF head_commit_id = \?.*?)\n\t\tAND deleted_at = null/$1/s'
    expect_red "C1 characterization detects a weaker witness CAS" "R1: certification=CERTIFIED/witness_applied" '^TestPCD1B4Characterization_InWindowLibraryLifecycle$' ./internal/integration/ -tags=integration
fi

echo "PC-D1B.4 guard mutations: every leg RED for its stated reason."
