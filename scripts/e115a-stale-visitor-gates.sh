#!/usr/bin/env bash
set -euo pipefail
cd /build
expect_red() {
 local name="$1" marker="$2"; shift 2
 local rc=0
 "$@" > "/tmp/e115a-gate-$name.log" 2>&1 || rc=$?
 if [ "$rc" -eq 0 ] || ! grep -qF "$marker" "/tmp/e115a-gate-$name.log" || grep -Eq 'build failed|syntax error' "/tmp/e115a-gate-$name.log"; then cat "/tmp/e115a-gate-$name.log"; exit 1; fi
 echo "PASS E1-15A gate negative control: $name"
}
expect_red filtered 'requires all named E1-15A stale visitor legs' env SESAMEFS_REQUIRE_E115A_STALE_VISITOR_EVIDENCE=1 go test -tags integration -count=1 -run '^TestStaleRepairVisitorCancellationCompleteness$' ./internal/integration
expect_red unavailable 'Backend not available' env SESAMEFS_REQUIRE_E115A_STALE_VISITOR_EVIDENCE=1 SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run '^TestStaleRepairVisitorCancellationCompleteness$' ./internal/integration
expect_red child_filtered 'requires all named E1-15A stale visitor legs' env SESAMEFS_REQUIRE_E115A_STALE_VISITOR_EVIDENCE=1 SESAMEFS_E115A_ISOLATED_URL=http://sesamefs-e19:8080 go test -tags integration -count=1 -run '^TestStaleRepairVisitorCancellation$/^retained-control$' ./internal/integration
