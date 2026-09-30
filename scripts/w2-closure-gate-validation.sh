#!/usr/bin/env bash
set -euo pipefail
cd /build
expect_failure() {
 local name="$1" marker="$2"; shift 2
 local rc=0
 "$@" > "/tmp/w2-gate-$name.log" 2>&1 || rc=$?
 [ "$rc" -ne 0 ] && grep -q "$marker" "/tmp/w2-gate-$name.log" || {
  cat "/tmp/w2-gate-$name.log"; echo "negative control failed: $name"; exit 1;
 }
 ! grep -Eq 'build failed|syntax error' "/tmp/w2-gate-$name.log" || exit 1
 echo "PASS fail-closed evidence control: $name"
}
expect_failure filtered 'requires all named W2-0 closure legs' env SESAMEFS_REQUIRE_W2_CLOSURE_EVIDENCE=1 \
 go test -tags integration -count=1 -run '^TestW2ClosureEvidenceCompleteness$' ./internal/integration
expect_failure unavailable 'Backend not available' env SESAMEFS_REQUIRE_W2_CLOSURE_EVIDENCE=1 SESAMEFS_URL=http://127.0.0.1:1 \
 go test -tags integration -count=1 -run '^TestW2ClosureEvidenceCompleteness$' ./internal/integration
expect_failure missing_legacy 'required pinned legacy binary missing' env SESAMEFS_REQUIRE_W2_ROLLOUT_EVIDENCE=1 SESAMEFS_W2_LEGACY_BINARY= \
 go test -tags integration -count=1 -run '^TestW2MixedRollout$' ./internal/integration
expect_failure filtered_3dc 'required real W2 repair 3-DC phase was not observed' env SESAMEFS_REQUIRE_W2_REPAIR_3DC_EVIDENCE=1 \
 go test -tags integration -count=1 -run '^TestW2ClosureEvidenceCompleteness$' ./internal/integration
