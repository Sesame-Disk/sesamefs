#!/usr/bin/env bash
# Run only in a disposable Docker test container: mutation never touches host.
set -euo pipefail
cd /build
export CASSANDRA_KEYSPACE=sesamefs_e19
export SESAMEFS_URL=http://sesamefs-e19:8080 SESAMEFS_URL_2=http://sesamefs-e19:8080 SESAMEFS_URL_3=http://sesamefs-e19:8080
export SESAMEFS_E113_CHILD=1
unset SESAMEFS_REQUIRE_E113_DISCOVERY_DELAY_EVIDENCE SESAMEFS_E113_ISOLATED_URL
file=internal/db/publication_liveness.go
cp "$file" /tmp/e113-publication-liveness.original
trap 'cp /tmp/e113-publication-liveness.original "$file"' EXIT
perl -0pi -e 's/return db\.blockHasPendingPublicationGlobal\(orgID, blockID\)/return false, nil \/\/ E1-11 omission control/' "$file"
! cmp -s "$file" /tmp/e113-publication-liveness.original || { echo 'mutation did not apply'; exit 1; }
rc=0
go test -tags integration -v -count=1 -timeout=2m -run '^TestRepairDiscoveryDelaySafety$/^target-not-yet-visited-gc$' ./internal/integration > /tmp/e113-mutation.log 2>&1 || rc=$?
cat /tmp/e113-mutation.log
[ "$rc" -ne 0 ] && grep -q 'E1-11 RED: pending repair allowed exact COMMITTED D(P1)' /tmp/e113-mutation.log || { echo 'mutation did not prove productive COMMITTED D'; exit 1; }
! grep -Eq 'build failed|syntax error|E1-11 teardown (blocks|block_references|gc_block_delete_lifecycles|gc_s3_orphans|commits|fs_objects|libraries_by_id|repairs|K1)|E1-12 expiry teardown (read|delete|canonical|projection)|E1-13 owner teardown (fs read|read|identity|delete|canonical|projection)|fixture.*teardown:' /tmp/e113-mutation.log || exit 1
grep -q 'E1-11 teardown verified' /tmp/e113-mutation.log || exit 1
grep -q 'E1-12 expiry teardown verified' /tmp/e113-mutation.log || exit 1
grep -q 'E1-13 owner teardown verified' /tmp/e113-mutation.log || exit 1
grep -q 'E1-13 target NOT visited:' /tmp/e113-mutation.log || exit 1
echo 'PASS: exact COMMITTED D caused only by omitting pending-repair guard; RED fixture teardown completed'
