#!/usr/bin/env bash
set -euo pipefail
cd /build
export SESAMEFS_REQUIRE_W2_PUBLICATION_CONTINUITY_EVIDENCE=1
pattern='^TestW2(Publication|Covered|SyncPublicationContinuity|WorkerRepairLifecycle)'
run(){ go test -tags integration -v -count=1 -timeout 10m -run "$pattern" ./internal/integration; }
run > /tmp/w2-continuity-baseline.log 2>&1 || { cat /tmp/w2-continuity-baseline.log; exit 1; }
cp internal/db/publication_liveness.go /tmp/w2-refs.orig
cp internal/api/v2/publish_repair.go /tmp/w2-repair.orig
cp internal/api/v2/files.go /tmp/w2-files.orig
cp internal/gc/worker.go /tmp/w2-worker.orig
restore_base(){ cp /tmp/w2-refs.orig internal/db/publication_liveness.go; cp /tmp/w2-repair.orig internal/api/v2/publish_repair.go; cp /tmp/w2-files.orig internal/api/v2/files.go; cp /tmp/w2-worker.orig internal/gc/worker.go; }
restore(){ restore_base; }
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
perl -0777 -i -pe 's/return db\.blockHasPendingPublicationGlobal\(orgID, blockID\)/return false, nil/ or die "gate mutation missed"' internal/db/publication_liveness.go
semantic_red omit_gc_gate
perl -0777 -i -pe 's/(func queuePendingPublishedFileRepairs\([^\n]+\{)/$1\n if true { return nil }/ or die "acquisition mutation missed"' internal/api/v2/publish_repair.go
semantic_red omit_repair_acquisition
perl -0777 -i -pe 's/(\n\s*W2PublicationAfterAuthorityBarrier\(repoID\))/\n clearPendingPublishedFileRepairs(h.db, orgID, repoID, commitID, pendingFiles)$1/ or die "release mutation missed"' internal/api/v2/files.go
semantic_red release_before_HEAD
worker_red(){
 local name="$1" marker="$2" rc=0
 run > "/tmp/w2-$name.log" 2>&1 || rc=$?
 [ "$rc" -ne 0 ] && grep -q "$marker" "/tmp/w2-$name.log" || { cat "/tmp/w2-$name.log"; exit 1; }
 ! grep -Eq 'build failed|syntax error' "/tmp/w2-$name.log" || exit 1
 echo "PASS semantic worker RED: $name"
 restore
}
perl -0777 -i -pe 's/hasRefs = liveness == db.BlockPublicationRealReference/hasRefs = liveness == db.BlockPublicationRealReference || liveness == db.BlockPublicationRepairGuardOnly/ or die "collapse missed"; s/if liveness == db.BlockPublicationRepairGuardOnly \{/if false \&\& liveness == db.BlockPublicationRepairGuardOnly {/ or die "guard missed"' internal/gc/worker.go
worker_red consume_repair_candidate 'W2 WORKER VIOLATION: repair-only consumed candidate'
perl -0777 -i -pe 's/(if alreadyCommitted \{\n\t\t)hasRefs, err = w.store.BlockHasReferencesGlobal\(item.OrgID, item.ItemID\)/${1}lateLiveness, lateErr := w.store.BlockPublicationLivenessGlobal(item.OrgID, item.ItemID)\n\t\thasRefs, err = lateLiveness != db.BlockPublicationZero, lateErr/ or die "committed mutation missed"' internal/gc/worker.go
worker_red repair_veto_after_D 'W2 WORKER VIOLATION: committed D stalled by late repair'
rc=0
go test -tags integration -count=1 -run '^TestW2PublicationLivenessThroughHEAD/expiryAfterAuthority$' ./internal/integration > /tmp/w2-filter.log 2>&1 || rc=$?
[ "$rc" -ne 0 ] && grep -q 'requires all named W2-0 legs' /tmp/w2-filter.log || exit 1
echo 'PASS filtered evidence fails closed'
rc=0
SESAMEFS_URL=http://127.0.0.1:1 go test -tags integration -count=1 -run "$pattern" ./internal/integration > /tmp/w2-unavailable.log 2>&1 || rc=$?
[ "$rc" -ne 0 ] && grep -q 'Backend not available' /tmp/w2-unavailable.log || exit 1
echo 'PASS unavailable backend fails closed'
echo 'W2-0 continuity: baseline GREEN, 3 semantic D+HEAD mutants RED, 2 worker lifecycle mutants RED, 2 evidence gates fail closed'
