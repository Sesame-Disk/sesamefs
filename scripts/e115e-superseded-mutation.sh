#!/usr/bin/env bash
# Run only in a disposable Docker test container: mutations never touch host.
# Each mutation removes one guard of the E1-15E superseded witness or its
# settlement; the named unit test must go RED with the named marker.
set -euo pipefail
cd /build
file=internal/api/v2/publish_repair.go
sed -i 's/\r$//' "$file"
cp "$file" /tmp/e115e-publish-repair.original
trap 'cp /tmp/e115e-publish-repair.original "$file"' EXIT
run_red() {
 local name="$1" test="$2" marker="$3" perl_expr="$4" check="$5"
 cp /tmp/e115e-publish-repair.original "$file"
 perl -0pi -e "$perl_expr" "$file"
 grep -q "$check" "$file" || { echo "$name mutation did not apply"; exit 1; }
 local rc=0
 go test -count=1 -run "$test" ./internal/api/v2 > "/tmp/e115e-$name.log" 2>&1 || rc=$?
 if [ "$rc" -eq 0 ] || ! grep -qF "$marker" "/tmp/e115e-$name.log" || grep -Eq 'build failed|syntax error|panic:' "/tmp/e115e-$name.log"; then
  cat "/tmp/e115e-$name.log"; echo "$name did not produce $marker"; exit 1
 fi
 echo "PASS E1-15E mutation $name: $marker"
}
# The witness must not fire when the SERIAL anchor is the parent itself.
run_red anchor '^TestSupersededRepairRetainedWhileHeadIsParent$' 'want retained UNKNOWN' \
 's/parent == "" \|\| anchor == "" \|\| parent == anchor \|\|/parent == "" || anchor == "" || \/\* E1-15E M-anchor \*\//' 'E1-15E M-anchor'
# Settlement must remove the repair-owned pub: before deleting the row.
run_red pub '^TestSupersededRepairSettlesOwnedPubThenRow$' 'row deleted before the repair-owned pub: was removed' \
 's/if err := removePublishedBlockReferenceRepairOwnedLivenessFn\(database, repair\); err != nil \{\n\t\t\treturn fmt\.Errorf\("remove repair-owned publish-attempt liveness for superseded/if err := error(nil); err != nil { \/\/ E1-15E M-pub\n\t\t\treturn fmt.Errorf("remove repair-owned publish-attempt liveness for superseded/' 'E1-15E M-pub'
# A superseded visit must never renew: drop its early settlement return.
run_red renew '^TestSupersededRepairSettlesOwnedPubThenRow$' 'want remove+delete only' \
 's/if classifyErr == nil && commitOutcome == publishedBlockReferenceRepairCommitSuperseded \{/if false \&\& classifyErr == nil \&\& commitOutcome == publishedBlockReferenceRepairCommitSuperseded { \/\/ E1-15E M-renew/' 'E1-15E M-renew'
# Without the witness the resumable classifier is back to UNKNOWN.
run_red witness '^TestSupersededRepairSettlesOwnedPubThenRow$' 'want settled' \
 's/if supersededAt != "" && currentCommitID == supersededAt \{/if false \&\& supersededAt != "" \&\& currentCommitID == supersededAt { \/\/ E1-15E M-witness/' 'E1-15E M-witness'
echo 'PASS: every E1-15E witness/settlement guard is load-bearing'
