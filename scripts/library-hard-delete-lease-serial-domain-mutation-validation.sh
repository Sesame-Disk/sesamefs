#!/usr/bin/env bash
# Directed removal mutation for ISSUE-GC-HARD-DELETE-LEASE-SERIAL-DOMAIN-01.
# The code test must go RED with the library hard-delete lease contract reason.
set -uo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

SOURCE=internal/gc/store_cassandra.go
BACKUP="$SOURCE.lease-domain.bak"
RUNNER=sesamefs-library-hard-delete-lease-serial-domain-mutation-runner
IMAGE=sesamefs-library-hard-delete-lease-serial-domain-mutation

green() { printf '\033[32m%s\033[0m\n' "$*"; }
red() { printf '\033[31m%s\033[0m\n' "$*" >&2; }
fail() { red "FAILED: $*"; exit 1; }

restore() {
	if [ -f "$BACKUP" ]; then
		cp -f "$BACKUP" "$SOURCE"
		rm -f "$BACKUP"
	fi
}

cleanup() {
	local rc=$?
	set +e
	restore
	docker rm -f "$RUNNER" >/dev/null 2>&1 || true
	exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

[ ! -e "$BACKUP" ] || fail "mutation backup already exists: $BACKUP"

docker build -f Dockerfile.gotest -t "$IMAGE" . || fail "Docker Go test image build failed"
docker rm -f "$RUNNER" >/dev/null 2>&1 || true
docker run -d --name "$RUNNER" --mount "type=bind,src=$PWD,dst=/workspace" --workdir /workspace "$IMAGE" sleep 3600 >/dev/null || fail "could not start Docker Go test runner"

docker exec "$RUNNER" cp "$SOURCE" "$BACKUP" || fail "could not back up the source in Docker"
docker exec "$RUNNER" perl -0pi -e 's{return query\.SerialConsistency\(gocql\.Serial\)}{return query}' "$SOURCE" || fail "could not apply the pin-removal mutation"
if cmp -s "$SOURCE" "$BACKUP"; then
	fail "pin-removal mutation did not change the source"
fi

set +e
output="$(docker exec "$RUNNER" go test ./internal/gc -count=1 -run '^TestLibraryHardDeleteLeasePinsGlobalSerial$' 2>&1)"
status=$?
set -e
if [ "$status" -eq 0 ]; then
	printf '%s\n' "$output"
	fail "removing SerialConsistency(gocql.Serial) stayed green"
fi
printf '%s\n' "$output" | grep -q "library hard-delete lease no longer pins global SERIAL" || {
	printf '%s\n' "$output"
	fail "pin-removal mutation failed for the wrong reason"
}
green "RED as required: library hard-delete lease no longer pins global SERIAL"
