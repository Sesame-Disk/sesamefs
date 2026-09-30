#!/usr/bin/env bash
# Directed mutations for ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01.
# Each mutation must turn the focused Go contract test RED for its own reason.
# Mutations run on the source copied into the Docker image; the host tree is
# never modified.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SOURCE=internal/gc/store_cassandra.go
BACKUP=/tmp/store_cassandra.go.orig
COMPOSE_PROJECT="$(docker compose -f docker-compose.yaml config 2>/dev/null | sed -n 's/^name: //p' | head -1)"
COMPOSE_PROJECT="${COMPOSE_PROJECT:-sesamefs}"
RUNNER="$COMPOSE_PROJECT-library-hard-delete-lease-serial-domain-mutation-runner"
IMAGE="$COMPOSE_PROJECT-library-hard-delete-lease-serial-domain-mutation"

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

docker build -f Dockerfile.gotest -t "$IMAGE" . || fail "Docker Go test image build failed"
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker run -d --name "$RUNNER" "$IMAGE" sleep 3600 >/dev/null || fail "could not start Docker Go test runner"

set +e
output="$(docker exec "$RUNNER" go test ./internal/gc -count=1 -run '^(TestHardDeleteLeasesPinGlobalSerial|TestIsAmbiguousHardDeleteLockCASError|TestSettleHardDeleteLockAcquire)$' 2>&1)"
status=$?
set -e
[ "$status" -eq 0 ] || { printf '%s\n' "$output"; fail "contract tests are not green before mutation"; }

docker exec "$RUNNER" cp "$SOURCE" "$BACKUP" || fail "could not back up the source in Docker"

# name%perl substitution%test regex%required failure reason
MUTATIONS=(
	'renew pin removed%s{(func renewHardDeleteLock.*?)\.SerialConsistency\(gocql\.Serial\)}{$1}s%^TestHardDeleteLeasesPinGlobalSerial$%hard-delete lease no longer pins global SERIAL'
	'release pin downgraded to LOCAL_SERIAL%s{(func releaseHardDeleteLock.*?)SerialConsistency\(gocql\.Serial\)}{$1SerialConsistency(gocql.LocalSerial)}s%^TestHardDeleteLeasesPinGlobalSerial$%hard-delete lease no longer pins global SERIAL'
	'stale takeover pin removed%s{(existingToken\.String\(\)\))\.SerialConsistency\(gocql\.Serial\)}{$1}%^TestHardDeleteLeasesPinGlobalSerial$%hard-delete lease no longer pins global SERIAL'
	'library acquire not settled%s#return settleHardDeleteLockAcquire\(acquired, err, func\(\) error \{\n\t\treturn ReleaseLibraryHardDeleteLockLease\(session, libraryID, leaseToken\)\n\t\}\)#return acquired, err#%^TestHardDeleteLeasesPinGlobalSerial$%ambiguous hard-delete lease acquire is not settled'
	'unknown outcome skips release%s#if releaseErr := release\(\); releaseErr != nil \{#if releaseErr := error(nil); releaseErr != nil {#%^TestSettleHardDeleteLockAcquire$%released=false'
	'CAS_WRITE_UNKNOWN classified as definite%s#errors\.As\(err, &casUnknown\) \|\| #(casUnknown != nil \&\& errors.As(err, \&casUnknown)) || #%^TestIsAmbiguousHardDeleteLockCASError$%cas write unknown: ambiguous = false'
)

for mutation in "${MUTATIONS[@]}"; do
	IFS='%' read -r name expr test_regex reason <<<"$mutation"
	docker exec "$RUNNER" cp "$BACKUP" "$SOURCE" || fail "could not restore the source in Docker"
	docker exec "$RUNNER" perl -0pi -e "$expr" "$SOURCE" || fail "could not apply mutation: $name"
	if docker exec "$RUNNER" cmp -s "$SOURCE" "$BACKUP"; then
		fail "mutation did not change the source: $name"
	fi
	set +e
	output="$(docker exec "$RUNNER" go test ./internal/gc -count=1 -run "$test_regex" 2>&1)"
	status=$?
	set -e
	if [ "$status" -eq 0 ]; then
		printf '%s\n' "$output"
		fail "mutation stayed green: $name"
	fi
	printf '%s\n' "$output" | grep -q -- "$reason" || {
		printf '%s\n' "$output"
		fail "mutation failed for the wrong reason: $name"
	}
	green "RED as required: $name"
done
