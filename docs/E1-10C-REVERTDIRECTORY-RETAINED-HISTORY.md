# E1-10c / RevertDirectory retained-history

## Frozen plan

Base main@a7548d2c0a75a89ad273ed714bfc86a93ad0266b (#260 merged).
Measure only one retained historical directory, one child file, one plaintext
block, same repo/org, absent current target, single DC, pre-HEAD. Existing
concurrency/parent tests are not exact-P/GC evidence. No production fence before
a supported RED; no conflict-policy matrix or historical retention removal.

Productive CreateDirectory, parent-bound HandleUpload and DeleteDirectory.
Capture commit/root/DIR1/FILE_FS1/SHA-256/SHA-1/exact P/K/bytes. Lapse only the
own upload up: as explicit expiry-state control with cleanup registered first.
Historical child fs: must remain the sole reference. Reuse #260's real-query
async deletion observer for this fresh tagless fixture; no assumed sleep.

Add separate integration-only historical and pre-HEAD hooks to RevertDirectory;
normal builds use no-ops. Historical entry is captured outside retry; instrument
after directory validation and immediately before actual HEAD CAS in the retry.
Do not substitute productive queries, add liveness, schema or runtime gates.

Three mandatory legs: normal, retained-history-gc, head-conflict. Actual worker
with fixture-controlled candidate discovery and productive reference reads must
observe positive LOCAL_QUORUM, settle candidate before claim/global proof/D,
leave canonical P/K/bytes and recovery state intact. Resume productive handler;
verify full named winning HEAD/root/DIR1/FILE_FS1/block/exact P/K/bytes chain,
inherited sole fs:, no pub:/repair/new fs: settlement. Real competitor before
first CAS; historical visits=1, HEAD attempts=2, winning parent=competitor,
both entries preserved. Normal/GC legs require one HEAD attempt.

Independent SESAMEFS_REQUIRE_E110C_REVERTDIR_CHARACTERIZATION gate and
^TestRevertDirectoryRetainedHistory selector (never ^TestE110C, which would
expand #259). Same-binary child uses existing manual-GC backend/keyspace,
filters other gates/child sentinels, preserves subtest filters and records
parent completion only after mandatory child success. No normal daemon changes.

Validate full matrix; five race repetitions; normal-only, unavailable,
active-daemon, historical-hook and pre-HEAD-hook omission rejection controls.
Restore runner source and verify source/image hashes. Both vet modes, gofmt,
whitespace and actual final Docker go-all-test; old file/directory/E1-6/E1-09
and daemon controls remain green, no normal GC-disabled skips. Audit all changes,
commit, push and create PR with reviewable evidence.

GREEN supports CLOSED-EVIDENCE only for the measured retained-history subset.
COMMITTED/TERMINAL unexecuted when retained fs: blocks D; never manufacture D.
If a supported RED appears, freeze it before the minimal funnel fix. RevertFile
prior disposition unchanged. W2-10/E1-10 overall/E1/X1 OPEN; broader shapes,
RevertDirents, retention/Phase5/6, R31, multi-DC, activation and #258 follow-ups
out of scope. Production GC OFF; normal dev daemon remains available. Measuring
four narrow handler examples cannot prove all residual risk is Phase5/6.

## Implementation and measured outcome

Plan frozen at 76faebb54. Independent integration hooks execute after validated
oldEntry==DIR1 outside retry and immediately before each real HEAD CAS. Normal
builds use no-ops; no publication protocol or production GC settings change.
Request uses real FileHandler.RevertDirectory, actual query p and JSON commit_id,
with same-org/owner permission context. External route/auth certification is not
claimed. Existing concurrency and nested-parent tests remain complementary.

Fixture reuses productive CreateDirectory and parent-bound HandleUpload, then
actual DeleteDirectory. #260's tagless fixture observer synchronizes completed
empty tag scan and final daily/library counter decrement before revert/cleanup;
no elapsed delay assumes completion. Deleted HEAD root must be a successfully
read persisted fs_objects row with valid JSON and no old-dir entry; traversal
errors or the helper's missing-row-as-empty fallback cannot certify absence.
Sole child fs: is inherited. Capture and
read back named historical/current commit/root/DIR1/FILE_FS1, ordered canonical
SHA-256/external SHA-1 block layout, exact P/K and bytes. Own up: removal is an
explicit expiry-state control, not elapsed TTL; projection cleanup is registered
before removal. Historical fs: and retirement state are never manufactured.

Normal leg succeeds without GC. GC leg and HEAD-conflict leg run the actual
worker using fixture-owned candidate discovery and productive Cassandra/SILO.
Completed LOCAL_QUORUM reference read is positive (one row); candidate is
settled. No global destructive probe/repair scan/claim/D/recovery root occurs.
Independent EACH_QUORUM RealReference observation is not a worker zero-proof.
Canonical exact P/K and bytes stay unchanged. Actual revert publishes original
DIR1/FILE_FS1 with no pub:/repair or fs: promotion. Real CreateFile competitor
wins before first CAS; historical visits=1, HEAD attempts=2, winning parent is
competitor, both named entries survive. Normal/GC require one HEAD attempt.
No retirement RED reproduced; COMMITTED/TERMINAL remain UNEXECUTED because
retained child fs: blocks the prerequisite. No runtime fence is justified.

Independent three-leg gate and same-binary child preserve subtest filters, clear
other evidence gates/child sentinels, and require child TestMain success before
parent certification. Reuses sesamefs-e19/sesamefs_e19; each child endpoint must
authentically report GC disabled. #259 ^TestE110 and #260 directory selector are
unchanged and cannot match TestRevertDirectoryRetainedHistory. Standard dev GC
remains available. Run integration binaries serially against this keyspace:
TestMain cleanup scans all inttest-* libraries, not just the running fixture.
Fixture IDs/bytes are fresh and each fixture registers its own cleanup.

CLOSED-EVIDENCE only for this one-directory/one-child/one-block/plaintext,
same-repo/org/retained-history/single-DC/pre-HEAD schedule. RevertFile prior
measured disposition unchanged. W2-10/E1-10 overall, RevertDirents, broader
shapes/policies, concurrent retention/Phase5/6, R31, E1/X1 and #258 follow-ups
remain OPEN. Four narrow handler examples cannot certify all residual risk
as exclusively retention/Phase5/6. Production GC remains OFF.

## Validation and audit

Final directed three-leg matrix, independent named-leg completeness and
TestMain gate wiring PASS (29.628s). Both normal and integration vet PASS.
Required normal-only filter fails with GC/conflict child legs missing and no
parent certificate. Unavailable backend fails before tests. Active-GC endpoint
fails authenticated enabled=false isolation before fixture creation.
Final restored-source matrix includes the strict persisted-root absence check.
Five serial race repetitions PASS (102.065s), exactly 15 named legs and no race
warnings. Legacy
#259/#260 required selectors PASS exactly three file and three directory legs;
no new RevertDirectory tests are captured. Docker gofmt and diff whitespace PASS.

Runner-only historical-hook omission fails all three legs with historical=0,
HEAD=1/1/2. Runner-only pre-HEAD-hook omission fails all three with historical=1,
HEAD=0, including failure to create the required real competing CAS schedule.
These are schedule sensitivity controls, not retirement RED or exact-P omission.
Runner restored after each mutation; no omitted hook enters the final image.

Frozen plan remains unchanged; host, image and restored runner hashes agree for
current Go sources (including unchanged prior matrices):

| Source | SHA-256 |
|---|---|
| internal/api/v2/files.go | 92fe6e82e13f869678819731937dc44f8d44063d8c9a6cf999038e518a441ffd |
| internal/api/v2/revertdirectory_publication_barriers.go | 144a8fe90ee1321272f4ead9e4249577f02bfab943dcec280d0194d311420ae1 |
| internal/api/v2/revertdirectory_publication_barriers_integration.go | 8b11e0307266eb7fdafa34cc8e5cf8d1608f9718da96146c54d337d4d0c33c86 |
| internal/integration/e110c_revertdirectory_retained_history_test.go | 142f07bdf7a74c2f949cc1648acc0a0e5443b8a4737a8f705eb52ca082643010 |
| internal/integration/integration_test.go | 4f87342730fba6c3bb95013129b3363dbd6b12018cde74d797c4b4431bc939da |
| internal/integration/e110_restoretrash_retained_history_test.go | 6ab7a37f1d0b55dbf1bf7349afb2337ae80dbd8103b5d0a68037b679cc4136af |
| internal/integration/e110b_restoretrash_directory_test.go | 1e2490bfb0440520b512e01f07e74dcf9ceda3559677b9c30cbca966b31c96d9 |

Logs are outside Git under $TEMP/sesamefs-e110c-*.log. Current test handlers run
against real Cassandra/SILO; external regressions use existing dev backends.
No production backend deployed. Optional multi-DC/cgroup/gcsoak evidence is not
certified. An initial full run was intentionally interrupted to strengthen the
persisted-root absence precondition; it is not a completion certificate. A race
run overlapped with negative/legacy binaries and was rejected: their global
TestMain cleanup deleted active inttest fixtures (DeleteDirectory 500 / revert
404; matching cleanup timestamps). No data-race warning occurred. Final evidence
is rerun serially; no publication protocol is changed. The cleanup adjustment
below addresses interrupted API fixtures; it does not make concurrent binaries
against the same keyspace safe.
A subsequent full run passed mandatory integration (861.732s) but Docker daemon
restart interrupted API before API/OIDC completion; validation runner also
exited 255 with OOMKilled=false. No overall PASS was claimed. Final unchanged
service is run detached with retained container state/logs to verify completion
independently of terminal/session lifetime. Container retention changes only
validation orchestration, not service commands, gates, timeout or configuration.
A further full run was stopped after quota failures: two cross-lib-src/dst API
fixtures survived the Docker interruption and consumed two of the three hard
library slots. No quota was enlarged. Existing cleanup-test-repos.sh deleted
only those owned fixtures through the real API. Go stale-fixture cleanup now
also recognizes the two explicit cross-lib-src-/cross-lib-dst- prefixes, while
liveRepoIDs protects the current process's fixtures. A regression rejects
non-fixture names. The previously affected block-mapping and BorrowedFS tests
plus cleanup regression PASS (34.843s), with unrelated mandatory gates unset
for that directed run. New E1-10c fixtures inherit early t.Cleanup registration
for library, exact object/mapping, block and GC artifacts. Library, uploaded
object/mapping and expiry cleanup report errors through testing.T; those failures
prevent the child exit certificate. Existing x1Cleanup has some best-effort
metadata deletes; this PR does not claim a general teardown proof for all
legacy fixtures. Library quota remains hard/3.
An earlier directed invocation printed test PASS but correctly exited nonzero
because unrelated mandatory gates were inherited; it is not a certificate.
Final actual standard Docker go-all-test PASS, retained container
sesamefs-e110c-final-go-all-2 exited 0: Go short/coverage, required integration
814.242s, API 20/20 suites and OIDC 25/25 tests. E1-10c 3/3; original file and
directory restore 3/3 each, RevertFile 6/6 and cross-repo 14/14. All 13 compiled
requireGCEnabled controls PASS with zero GC-disabled skips. The separately
tagged gcsoak test is outside the standard command and is not certified.
Final normal/integration vet, gofmt, whitespace, frozen-plan prefix and
host/image/runner Go source hash checks PASS. Scoped audit found no unresolved
introduced P0/P1/P2. The published claim remains retained-history only.
After the complete run, cleanup-test-repos.sh against both standard and manual
backends found no owned test repositories to delete; admin repo lists are empty
on both. Default organization remains hard/free with the existing three-library
profile. No quota, GC activation or shared daemon setting was changed.
