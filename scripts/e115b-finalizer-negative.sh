#!/usr/bin/env bash
# Run only in a disposable Docker test container: the injection never touches host.
# Focused negative control: fail inside e115aCommitD right after exact COMMITTED
# D(P1) is verified. The early conditional finalizer must still complete the
# owned root to TERMINAL, and every teardown verifier must pass (no leaked root).
set -euo pipefail
cd /build
export CASSANDRA_KEYSPACE=sesamefs_e19
export SESAMEFS_URL=http://sesamefs-e19:8080 SESAMEFS_URL_2=http://sesamefs-e19:8080 SESAMEFS_URL_3=http://sesamefs-e19:8080
export SESAMEFS_E115B_CHILD=1
unset SESAMEFS_E115B_ISOLATED_URL $(env | grep -o '^SESAMEFS_REQUIRE_[A-Z0-9_]*' || true)
file=internal/integration/e115a_stale_repair_visitor_test.go
cp "$file" /tmp/e115b-finalizer.original
trap 'cp /tmp/e115b-finalizer.original "$file"' EXIT
perl -0pi -e 's/(\tt\.Logf\("E1-15A natural D: )/\tt.Fatalf("E1-15B injected failure after exact COMMITTED") \/\/ E1-15B finalizer negative\n$1/' "$file"
grep -q 'E1-15B finalizer negative' "$file" || { echo 'injection did not apply'; exit 1; }
rc=0
go test -tags integration -v -count=1 -timeout=4m -run '^TestWriterPostDStagingCrash$/^d-before-stage-no-crash$' ./internal/integration > /tmp/e115b-finalizer.log 2>&1 || rc=$?
cat /tmp/e115b-finalizer.log
[ "$rc" -ne 0 ] && grep -q 'E1-15B injected failure after exact COMMITTED' /tmp/e115b-finalizer.log || { echo 'injected post-COMMITTED failure not observed'; exit 1; }
grep -q 'E1-15B finalizer completed exact COMMITTED root' /tmp/e115b-finalizer.log || { echo 'finalizer did not complete the owned root'; exit 1; }
! grep -Eq 'build failed|syntax error|panic:|E1-15A teardown (roots read|recovery root)|E1-14 teardown (candidates|candidate projection|queue|moved projection)|E1-11 teardown (blocks|block_references|gc_block_delete_lifecycles|gc_s3_orphans|commits|fs_objects|libraries_by_id|repairs|K1)|E1-12 expiry teardown (read|delete|canonical|projection)|E1-13 owner teardown (fs read|read|identity|delete|canonical|projection)|fixture.*teardown:' /tmp/e115b-finalizer.log || exit 1
for verified in 'E1-15A teardown verified' 'E1-14 teardown verified' 'E1-11 teardown verified' 'E1-12 expiry teardown verified' 'E1-13 owner teardown verified'; do
 grep -q "$verified" /tmp/e115b-finalizer.log || { echo "missing $verified"; exit 1; }
done
echo 'PASS: failure after exact COMMITTED inside e115aCommitD; early finalizer completed the root; no leaked root/orphan/K1'
