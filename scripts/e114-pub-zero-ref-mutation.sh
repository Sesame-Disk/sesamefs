#!/usr/bin/env bash
# Run only in a disposable Docker test container: mutation never touches host.
# Causal control, not a fix: TTL-bound pub: writes temporarily go through the
# up: expiry-projection writer. The unchanged final-expiry leg must then reach a
# productive Phase 0 candidate (never D) and still tear everything down.
set -euo pipefail
cd /build
export CASSANDRA_KEYSPACE=sesamefs_e19
export SESAMEFS_URL=http://sesamefs-e19:8080 SESAMEFS_URL_2=http://sesamefs-e19:8080 SESAMEFS_URL_3=http://sesamefs-e19:8080
export SESAMEFS_E114_CHILD=1
unset SESAMEFS_REQUIRE_E114_PUB_ZERO_REF_EVIDENCE SESAMEFS_E114_ISOLATED_URL
file=internal/db/block_references.go
cp "$file" /tmp/e114-block-references.original
trap 'cp /tmp/e114-block-references.original "$file"' EXIT
perl -0pi -e 's/(func \(db \*DB\) AddBlockReference\(orgID, blockID, referrer, libraryID string, ttlSeconds int\) error \{\r?\n)/$1\tif ttlSeconds > 0 && strings.HasPrefix(referrer, blockReferrerPublishPrefix) { \/\/ E1-14 projection mutation\n\t\tvar class string\n\t\tif err := db.Session().Query(`SELECT storage_class FROM blocks WHERE org_id = ? AND block_id = ?`, orgID, blockID).Consistency(gocql.LocalQuorum).Scan(&class); err != nil {\n\t\t\treturn err\n\t\t}\n\t\treturn db.AddProvisionalBlockReferenceWithExpiry(orgID, blockID, referrer, libraryID, class, time.Now().Add(time.Duration(ttlSeconds)*time.Second))\n\t}\n/' "$file"
grep -q 'E1-14 projection mutation' "$file" || { echo 'mutation did not apply'; exit 1; }
rc=0
go test -tags integration -v -count=1 -timeout=3m -run '^TestPubZeroRefTransition$/^final-pub-expiry-no-repair$' ./internal/integration > /tmp/e114-mutation.log 2>&1 || rc=$?
cat /tmp/e114-mutation.log
[ "$rc" -ne 0 ] && grep -q 'E1-14 DISCOVERED (Phase 0 after final pub: expiry): productive zero-ref candidate' /tmp/e114-mutation.log || { echo 'mutation did not make final pub: expiry discoverable'; exit 1; }
grep -q 'E1-14 up: expired by TTL; Phase 0 resolved its projection; pub-only' /tmp/e114-mutation.log || exit 1
! grep -Eq 'build failed|syntax error|panic:|E1-14 teardown (candidates|candidate projection|queue|moved projection)|E1-11 teardown (blocks|block_references|gc_block_delete_lifecycles|gc_s3_orphans|commits|fs_objects|libraries_by_id|repairs|K1)|E1-12 expiry teardown (read|delete|canonical|projection)|E1-13 owner teardown (fs read|read|identity|delete|canonical|projection)|fixture.*teardown:' /tmp/e114-mutation.log || exit 1
grep -q 'E1-14 teardown verified' /tmp/e114-mutation.log || exit 1
grep -q 'E1-11 teardown verified' /tmp/e114-mutation.log || exit 1
grep -q 'E1-12 expiry teardown verified' /tmp/e114-mutation.log || exit 1
grep -q 'E1-13 owner teardown verified' /tmp/e114-mutation.log || exit 1
echo 'PASS: final pub: expiry becomes a productive candidate only with a pub: expiry projection; mutation fixture teardown completed'
