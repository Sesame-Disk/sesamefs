#!/usr/bin/env bash
set -euo pipefail
cd /build
expect_red() {
 local name="$1" marker="$2"; shift 2
 local rc=0
 "$@" > "/tmp/e115b-gate-$name.log" 2>&1 || rc=$?
 if [ "$rc" -eq 0 ] || ! grep -qF "$marker" "/tmp/e115b-gate-$name.log" || grep -Eq 'build failed|syntax error' "/tmp/e115b-gate-$name.log"; then cat "/tmp/e115b-gate-$name.log"; exit 1; fi
 echo "PASS E1-15B gate negative control: $name"
}
expect_red filtered 'requires all named E1-15B writer post-D legs' env SESAMEFS_REQUIRE_E115B_WRITER_POST_D_EVIDENCE=1 go test -tags integration -count=1 -run '^TestWriterPostDStagingCrashCompleteness$' ./internal/integration
expect_red unavailable 'Backend not available' env SESAMEFS_REQUIRE_E115B_WRITER_POST_D_EVIDENCE=1 SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run '^TestWriterPostDStagingCrashCompleteness$' ./internal/integration
expect_red child_filtered 'requires all named E1-15B writer post-D legs' env SESAMEFS_REQUIRE_E115B_WRITER_POST_D_EVIDENCE=1 SESAMEFS_E115B_ISOLATED_URL=http://sesamefs-e19:8080 go test -tags integration -count=1 -run '^TestWriterPostDStagingCrash$/^no-gc-control$' ./internal/integration
