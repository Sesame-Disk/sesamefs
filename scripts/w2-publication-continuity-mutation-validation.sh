#!/usr/bin/env bash
set -euo pipefail
cd /build
export SESAMEFS_REQUIRE_W2_PUBLICATION_CONTINUITY_EVIDENCE=1
pattern='^TestW2(Publication|Covered|SyncPublicationContinuity)'
run(){ go test -tags integration -v -count=1 -timeout 10m -run "$pattern" ./internal/integration; }
run > /tmp/w2-continuity-baseline.log 2>&1 || { cat /tmp/w2-continuity-baseline.log; exit 1; }
cp internal/db/block_references.go /tmp/w2-refs.orig
cp internal/api/v2/publish_repair.go /tmp/w2-repair.orig
cp internal/api/v2/files.go /tmp/w2-files.orig
restore(){ cp /tmp/w2-refs.orig internal/db/block_references.go; cp /tmp/w2-repair.orig internal/api/v2/publish_repair.go; cp /tmp/w2-files.orig internal/api/v2/files.go; }
trap restore EXIT
semantic_red(){
 local name="$1";local rc=0
 run > "/tmp/w2-$name.log" 2>&1 || rc=$?
 [ "$rc" -ne 0 ] || { echo "mutant stayed green: $name";exit 1; }
 grep -q 'W2-0 VIOLATION: D(P) committed AND HEAD advanced' "/tmp/w2-$name.log" || { cat "/tmp/w2-$name.log";echo "not semantic D+HEAD RED: $name";exit 1; }
 ! grep -Eq 'build failed|syntax error' "/tmp/w2-$name.log" || exit 1
 echo "PASS semantic D+HEAD RED: $name"
 restore
}
perl -0777 -i -pe 's/return db\.blockHasPendingPublicationGlobal\(orgID, blockID\)/return false, nil/ or die "gate mutation missed"' internal/db/block_references.go
semantic_red omit_gc_gate
perl -0777 -i -pe 's/(func queuePendingPublishedFileRepairs\([^\n]+\{)/$1\n if true { return nil }/ or die "acquisition mutation missed"' internal/api/v2/publish_repair.go
semantic_red omit_repair_acquisition
perl -0777 -i -pe 's/(\n\s*W2PublicationAfterAuthorityBarrier\(repoID\))/\n clearPendingPublishedFileRepairs(h.db, orgID, repoID, commitID, pendingFiles)$1/ or die "release mutation missed"' internal/api/v2/files.go
semantic_red release_before_HEAD
rc=0
go test -tags integration -count=1 -run '^TestW2PublicationLivenessThroughHEAD/expiryAfterAuthority$' ./internal/integration > /tmp/w2-filter.log 2>&1 || rc=$?
[ "$rc" -ne 0 ] && grep -q 'requires all named W2-0 legs' /tmp/w2-filter.log || exit 1
echo 'PASS filtered evidence fails closed'
rc=0
SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run "$pattern" ./internal/integration > /tmp/w2-unavailable.log 2>&1 || rc=$?
[ "$rc" -ne 0 ] && grep -q 'Backend not available' /tmp/w2-unavailable.log || exit 1
echo 'PASS unavailable backend fails closed'
echo 'W2-0 continuity: baseline GREEN, 3 semantic D+HEAD mutants RED, 2 evidence gates fail closed'
