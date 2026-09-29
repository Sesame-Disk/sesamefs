# W2-6a: CreateFile Office-template exact placement before HEAD

Scope: `.docx`, `.xlsx`, `.pptx` publication in `CreateFile` only.
Base: main@63aa84573 (merged PR #237). Directed race validation keeps GC disabled.
The full local suite uses the development-only worker/scanner exception in Compose.

## Plan and acceptance evidence

1. RED first: real Cassandra/MinIO, production in-process CreateFile. For each
   Office extension, exercise writerFirst, gcCommittedBeforeStage, and
   gcFullyRetiredBeforeStage. Model expiry by removing the request's own up:,
   commit D only after EACH_QUORUM zero refs, and physically retire P in the
   terminal leg. Isolate organization/library identities because template hashes
   are shared. Preserve the empty-file behavior with a separate blockless leg.
2. Minimal fix: carry the existing materialized SHA-256/class/key to
   validateCommitBlockPublicationFences, after durable pub: and immediately
   before HEAD CAS. Reuse existing cleanup and retryable 409 handling.
3. GREEN: require HEAD unchanged, no permanent fs:, pub: removed, D unchanged
   or fully settled/absent in the GC legs; require durable pub: before successful
   HEAD. Exercise template reuse as well as a fresh placement.
4. Mutation/evidence: omit the placement and corrupt its storage_key in isolated
   Docker copies. Require semantic test failures, not compilation failures.
   Require every named leg; a filtered run or unavailable backend fails closed.
5. Docker checks: formatting, short package suite, vet, full integration profile,
   and scoped mutation script. Audit the final diff and docs before commit/push.
6. Close only W2-6a pre-HEAD in X1 and align its existing issue references.
   W2-0 stays OPEN. Create a PR against main with RED/GREEN/mutation evidence.

Excluded: W2-7/8/9/10, Sync W2-3/4/5, R31 W2-11..14, PC-D1B.5, G4, PRE-GC.
The publication protocol, tables and coordinator are unchanged.

## Results

- RED before the fix (2026-09-29 22:25 UTC): the six GC races returned 201 and
  advanced HEAD. All three writerFirst controls and emptyFile passed.
- Directed GREEN after the fix: nine named Office legs and emptyFile passed;
  writerFirst also creates a second file from the same physical template key.
  The gate-wiring and named-completeness tests passed. Latest fixture checks
  additionally verify exact object bytes/physical absence and remove the
  library read models and expiry projections that the test owns.
- `go test ./... -short -cover -count=1` and `go vet ./...`: passed in Docker.
- Scoped mutations: omitting the placement reproduces the unsafe 201/HEAD;
  corrupting storage_key rejects writerFirst with 409; passing SHA-1 instead
  of SHA-256 rejects it with 500 (invalid internal identity). All three are
  semantic RED, not build failures. Filtered evidence and an unavailable
  backend both fail closed. The final fixture version passed the baseline and all five negative legs.
- Formatting (`gofmt -l`) and mutation script syntax (`bash -n`): clean in Docker.
- `go test -race -short -count=1 -timeout 10m ./internal/api/v2 ./internal/db`:
  GREEN in Docker (4.313s / 59.921s); no races.
- Final scope audit in Docker compared normalized bytes with the Git base:
  `files.go` outside CreateFile is identical; every other W2 exit row is
  identical; W2-0 stays OPEN and W2-6a is CLOSED-FIX pre-HEAD.
- After the full local profile, GC was restored to false on all three nodes.
  The final mutation run used that disabled-GC stack and passed 3/3 production
  mutations plus 2/2 fail-closed gate checks. No source changes followed it.
- First full profile with every node GC-disabled: 434 PASS, 79 SKIP, one failure
  because the share-link scanner test needs the local scanner (manual trigger
  returns 503 with GC disabled). W2-6a passed. This is a test environment mismatch,
  not a production/funnel change. Rerun uses the supported local Compose setup:
  primary worker/scanner enabled, nodes 2/3 disabled. Final result: GREEN, 448
  top-level PASS, 66 conditional SKIP, zero FAIL (`internal/integration`
  349.525s). All nine Office legs and emptyFile ran and passed; the scanner
  regression passed too (2.34s). The optional 3-DC/special-purpose drills were
  not enabled. The full profile used the rebuilt application on all three nodes.

## Reproduction

For directed race/mutation checks, disable GC on all three application nodes
with a local Compose override. The validation used `tmp/w2-6a-test.compose.yaml` with
`GC_ENABLED: "false"` for `sesamefs`, `sesamefs-node-2`, and `sesamefs-node-3`;
the temporary override is not part of this PR. Cassandra uses LOCAL_QUORUM,
global SERIAL and the existing single-DC development keyspace; MinIO is real.
The full suite also contains a manual GC scanner test. Use the development-only
configuration documented in docker-compose.yaml (primary GC worker/scanner
allowed, other nodes disabled) for that full profile, then restore the directed
override. Production GC activation remains prohibited. The fix reuses the
established W2-0 ordering proof and adds no 3-DC protocol.

```sh
docker compose --profile test build go-integration-test
docker compose up -d --no-deps sesamefs sesamefs-node-2 sesamefs-node-3
docker compose --profile test run --rm --no-deps go-integration-test
docker compose -f docker-compose.yaml -f tmp/w2-6a-test.compose.yaml \
  up -d --no-deps sesamefs sesamefs-node-2 sesamefs-node-3
docker run --rm --network sesamefs_default --env-file .env \
  -e SESAMEFS_URL=http://sesamefs:8080 sesamefs-go-integration-test \
  bash scripts/w2-createfile-office-exact-p-mutation-validation.sh
```

The integration evidence gate is
`SESAMEFS_REQUIRE_W2_CREATEFILE_EXACT_P_EVIDENCE=1`. It is enabled in the normal
integration/all-test Compose commands and requires all nine extension/race
pairs plus emptyFile, even when `-run` excludes the test or TestMain cannot
reach the backend. The mutation script requires this gate independently.

## Scope audit

The productive diff in `files.go` is wholly inside `CreateFile`. It adds one
placement list (SHA-256 + materialized class/key) and calls the existing final
validator immediately before HEAD, after staging pub: and durable repair.
Rejection uses the existing failed-attempt cleanup and clears queued repair.
No materialization algorithm, CQL, schema, coordinator or GC worker changes.
Production barrier functions are no-ops; their callbacks exist only with the
integration build tag. Empty files pass no placements (zero added DB reads).

Cost: two LOCAL_QUORUM point reads per Office-template HEAD attempt, repeated
on HEAD-conflict retries, using the existing bounded validator. All other
funnel code and W2 exit states are unchanged. W2-0 stays OPEN, W2-6a closes
only pre-HEAD, and post-HEAD R31 remains OPEN.

## Merge requirement audit

| Requirement | Evidence |
|---|---|
| Bug reproduced before the fix | Real Cassandra/MinIO RED: six GC cases returned 201 and moved HEAD |
| Minimal existing exact-P mechanism | Existing placement type and validator; 19 added lines only in CreateFile |
| Real Cassandra/MinIO GREEN | Nine extension/race pairs + emptyFile; 448 PASS in the supported full profile |
| Guard mutation proves the check | Omitted placement, wrong storage_key and wrong internal hash all semantic RED |
| Evidence cannot silently skip | Filtered required legs and unreachable backend both exit nonzero |
| GC rejection invariants | 409, HEAD unchanged, pub: removed, no own fs:, D unrevoked / canonical P absent; physical retirement checked |
| Empty CreateFile preserved | 201, size zero, no block IDs, no materialization hook |
| W2-6a closes alone | X1 CLOSED-FIX pre-HEAD; all other exit rows identical, W2-0 still OPEN |
| Final audit | Source/scope checks passed; this PR delivers the verified branch against main |
