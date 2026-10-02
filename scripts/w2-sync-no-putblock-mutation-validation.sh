#!/usr/bin/env bash
# Run inside the Docker Go runner against the isolated Cassandra/MinIO stack.
# Mutations affect only the container's copied sources, never a host mount.
set -euo pipefail
cd /build
export SESAMEFS_REQUIRE_W24_CHARACTERIZATION=1
pattern='^TestW2SyncNoPutBlock$'
original="$(mktemp /tmp/w24-sync.XXXXXX.go)"
cp internal/api/sync.go "$original"
restore() { cp "$original" internal/api/sync.go; }
cleanup() { restore; rm -f "$original"; }
trap cleanup EXIT
run() { go test -tags integration -count=1 -v -timeout 10m -run "$pattern" ./internal/integration; }
run > /tmp/w24-mutation-green.log 2>&1 || { cat /tmp/w24-mutation-green.log; exit 1; }
semantic_red() {
	local name="$1" status=0
	run > "/tmp/w24-mutation-$name.log" 2>&1 || status=$?
	if [ "$status" -eq 0 ] || ! grep -q 'W2-4 VIOLATION: D(P) committed AND HEAD advanced' "/tmp/w24-mutation-$name.log"; then
		cat "/tmp/w24-mutation-$name.log"
		echo "mutation did not produce semantic RED: $name" >&2
		exit 1
	fi
	if grep -Eq 'build failed|syntax error' "/tmp/w24-mutation-$name.log"; then
		cat "/tmp/w24-mutation-$name.log"
		exit 1
	fi
	echo "PASS semantic D(P)+HEAD RED: $name"
	restore
}
perl -0777 -i -pe 's/h\.resolveSyncCommitBlockPlacements\(orgID, syncCommitBlockIDUnion\(canonicalByFile\)\)/h.resolveSyncCommitBlockPlacements(orgID, provenanced)/ or die "scope mutation missed"' internal/api/sync.go
semantic_red provenance_only_placements
perl -0777 -i -pe 's/(func \(h \*SyncHandler\) prepareSyncCommitBlockPublicationReadiness\([^\n]+\{.*?\n\t)return placements, nil/${1}return nil, nil/s or die "capture mutation missed"' internal/api/sync.go
semantic_red discard_captured_placements
echo 'W2-4 baseline GREEN; provenance-only and discarded-placement mutants are semantic RED'
