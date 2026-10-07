#!/usr/bin/env bash
# Run only in a disposable Docker test container: mutations never touch host.
# M-fix reverts the E1-15A global-absence withdrawal: clear-after-recheck must
# reproduce the current-runtime RED. M-pre makes the durable pending re-check
# treat an absent row as pending: clear-before-recheck must go RED.
set -euo pipefail
cd /build
export CASSANDRA_KEYSPACE=sesamefs_e19
export SESAMEFS_URL=http://sesamefs-e19:8080 SESAMEFS_URL_2=http://sesamefs-e19:8080 SESAMEFS_URL_3=http://sesamefs-e19:8080
export SESAMEFS_E115A_CHILD=1
unset SESAMEFS_REQUIRE_E115A_STALE_VISITOR_EVIDENCE SESAMEFS_E115A_ISOLATED_URL
file=internal/api/v2/publish_repair.go
cp "$file" /tmp/e115a-publish-repair.original
trap 'cp /tmp/e115a-publish-repair.original "$file"' EXIT
run_red() {
 local name="$1" leg="$2" marker="$3" perl_expr="$4" check="$5"
 cp /tmp/e115a-publish-repair.original "$file"
 perl -0pi -e "$perl_expr" "$file"
 grep -q "$check" "$file" || { echo "$name mutation did not apply"; exit 1; }
 local rc=0
 go test -tags integration -v -count=1 -timeout=4m -run "^TestStaleRepairVisitorCancellation\$/^$leg\$" ./internal/integration > "/tmp/e115a-$name.log" 2>&1 || rc=$?
 cat "/tmp/e115a-$name.log"
 [ "$rc" -ne 0 ] && grep -qF "$marker" "/tmp/e115a-$name.log" || { echo "$name did not produce $marker"; exit 1; }
 grep -q 'E1-15A natural D: .* reached exact COMMITTED D(P1)' "/tmp/e115a-$name.log" || { echo "$name: no exact COMMITTED before resume"; exit 1; }
 ! grep -Eq 'build failed|syntax error|panic:|E1-15A teardown (roots read|recovery root)|E1-14 teardown (candidates|candidate projection|queue|moved projection)|E1-11 teardown (blocks|block_references|gc_block_delete_lifecycles|gc_s3_orphans|commits|fs_objects|libraries_by_id|repairs|K1)|E1-12 expiry teardown (read|delete|canonical|projection)|E1-13 owner teardown (fs read|read|identity|delete|canonical|projection)|fixture.*teardown:' "/tmp/e115a-$name.log" || exit 1
 for verified in 'E1-15A teardown verified' 'E1-14 teardown verified' 'E1-11 teardown verified' 'E1-12 expiry teardown verified' 'E1-13 owner teardown verified'; do
  grep -q "$verified" "/tmp/e115a-$name.log" || { echo "$name missing $verified"; exit 1; }
 done
 echo "PASS E1-15A mutation $name: $marker"
}
run_red fix clear-after-recheck 'E1-15A RED (clear-after-recheck): stale visitor left a durable reference after exact COMMITTED D(P1)' \
 's/gone, goneErr := publishedBlockReferenceRepairGoneGloballyFn\(database, repair\)/gone, goneErr := false, error(nil) \/\/ E1-15A M-fix/' 'E1-15A M-fix'
run_red pre clear-before-recheck 'E1-15A RED (clear-before-recheck): stale visitor left a durable reference after exact COMMITTED D(P1)' \
 's/(func publishedBlockReferenceRepairStillPending\(database \*db\.DB, repair publishedBlockReferenceRepair\) \(bool, error\) \{\r?\n\t_, err := loadLivePublishedBlockReferenceRepair\(database, repair\)\r?\n\tif err == nil \{\r?\n\t\treturn true, nil\r?\n\t\}\r?\n\tif errors\.Is\(err, errPublishedBlockReferenceRepairGone\) \{\r?\n\t\treturn )false, nil/$1true, nil \/\/ E1-15A M-pre/' 'E1-15A M-pre'
echo 'PASS: both E1-15A causal mutations reproduce durable post-D references; teardown completed'
