#!/usr/bin/env bash
# Run inside the dedicated Docker runner, never modify productive baseline code.
set -euo pipefail
cd /build
expected=50c50903e7ac49c04ef36f460dccc35ce122dfd6
actual=$(git get-tar-commit-id < tmp/w2-legacy-baseline.tar)
[ "$actual" = "$expected" ] || { echo 'wrong pre-#239 archive identity'; exit 1; }
echo "Pinned legacy source: $actual"
baseline=/tmp/w2-legacy-source
mkdir -p "$baseline"
tar -xf tmp/w2-legacy-baseline.tar -C "$baseline"
cp internal/db/publication_evidence_integration.go "$baseline/internal/db/"
cp internal/integration/w2_legacy_rollout_child_test.go "$baseline/internal/integration/"
perl -0777 -i -pe 's/func TestMain\(m \*testing.M\) \{/func TestMain(m *testing.M) {\n if os.Getenv("SESAMEFS_W2_LEGACY_CHILD") == "1" { os.Exit(m.Run()) }/ or die "legacy TestMain entry missed"' "$baseline/internal/integration/integration_test.go"
(cd "$baseline" && go test -tags integration -c -o /tmp/w2-legacy.test ./internal/integration)
export SESAMEFS_W2_LEGACY_BINARY=/tmp/w2-legacy.test
export SESAMEFS_REQUIRE_W2_ROLLOUT_EVIDENCE=1
go test -tags integration -count=1 -v -timeout 5m -run '^TestW2MixedRollout' ./internal/integration
