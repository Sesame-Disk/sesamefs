#!/usr/bin/env bash
set -euo pipefail
cd /build
expect_red() {
 local name="$1" marker="$2"; shift 2
 local rc=0
 "$@" > "/tmp/e113-gate-$name.log" 2>&1 || rc=$?
 if [ "$rc" -eq 0 ] || ! grep -qF "$marker" "/tmp/e113-gate-$name.log" || grep -Eq 'build failed|syntax error' "/tmp/e113-gate-$name.log"; then cat "/tmp/e113-gate-$name.log"; exit 1; fi
 echo "PASS E1-13 gate negative control: $name"
}
expect_red filtered 'requires all named E1-13 discovery delay legs' env SESAMEFS_REQUIRE_E113_DISCOVERY_DELAY_EVIDENCE=1 go test -tags integration -count=1 -run '^TestRepairDiscoveryDelaySafetyCompleteness$' ./internal/integration
expect_red unavailable 'Backend not available' env SESAMEFS_REQUIRE_E113_DISCOVERY_DELAY_EVIDENCE=1 SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run '^TestRepairDiscoveryDelaySafetyCompleteness$' ./internal/integration
expect_red child_filtered 'requires all named E1-13 discovery delay legs' env SESAMEFS_REQUIRE_E113_DISCOVERY_DELAY_EVIDENCE=1 SESAMEFS_E113_ISOLATED_URL=http://sesamefs-e19:8080 go test -tags integration -count=1 -run '^TestRepairDiscoveryDelaySafety$/^control-no-backlog$' ./internal/integration
