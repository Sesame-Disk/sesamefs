#!/usr/bin/env bash
set -euo pipefail
cd /build
expect_red() {
 local name="$1" marker="$2"; shift 2
 local rc=0
 "$@" > "/tmp/g5-coexistence-gate-$name.log" 2>&1 || rc=$?
 if [ "$rc" -eq 0 ] || ! grep -qF "$marker" "/tmp/g5-coexistence-gate-$name.log" || grep -Eq 'build failed|syntax error' "/tmp/g5-coexistence-gate-$name.log"; then cat "/tmp/g5-coexistence-gate-$name.log"; exit 1; fi
 echo "PASS G5 gate negative control: $name"
}
expect_red filtered 'required G5 isolated old-life continuation evidence was not observed' env SESAMEFS_REQUIRE_G5_COEXISTENCE_EVIDENCE=1 go test -tags integration -count=1 -run '^TestEveryEvidenceGateIsWiredIntoTestMain$' ./internal/integration
expect_red unavailable 'Backend not available' env SESAMEFS_REQUIRE_G5_COEXISTENCE_EVIDENCE=1 SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run '^TestEveryEvidenceGateIsWiredIntoTestMain$' ./internal/integration
