# E1-10d / W2-10 — RevertDirents retained-history

## Frozen plan

Base main@f465d1405d147dc34e69c894c08c21b435df5c1d (#262 merged).
Evidence first, no speculative runtime fence. Single path, historical file,
single plaintext block, root parent, same repo/org, absent current target,
retained commit, single DC, pre-HEAD. No batch, directory, nested parent,
fallback, replacement, quota partial-success, multiblock, retention Phase5/6,
R31, multi-DC or GC activation claim.

Productive upload and DeleteFile identify history/root/FILE_FS1, ordered
SHA-256/SHA-1 layout, exact P/K/bytes and sole inherited permanent fs:.
Lapse only own up: as explicit expiry-state control; register expiry cleanup
before removal. Require persisted deleted root and target absence, not a
missing-row fallback. Observe productive delete housekeeping before teardown.

Add independent integration hooks after oldEntry capture (outside per-item
retry) and immediately before actual HEAD CAS. Include repoID/itemPath/fsID;
normal builds are no-ops. Call actual RevertDirents with form commit_id/path
and owner context; do not claim external auth/routing coverage.

Three required legs: normal; retained-history-gc (real worker positive local
reference settles controlled exact candidate before claim/global proof/D);
head-conflict (real empty CreateFile wins first CAS, historical capture once,
HEAD attempts twice, winning parent equals competing HEAD, both names remain).
Assert success exactly one normalized file path and failed empty, full named
HEAD/root/FILE_FS1/layout/P/K/bytes, sole inherited fs:, no pub:/repair/settlement
and no D/recovery root. Never delete historical fs: or manufacture retirement.

Use independent gate SESAMEFS_REQUIRE_E110D_REVERTDIRENTS_CHARACTERIZATION and
selector ^TestRevertDirentsRetainedHistory. Same-binary isolated child on
sesamefs-e19/sesamefs_e19, authenticated GC OFF; leave dev daemon unchanged.
Keep existing selectors/gates intact. Only certificate after child cleanup and
exit 0. Early owned library/object/mapping/block/expiry cleanup even on failure.
Run integration binaries serially against shared keyspace; do not raise quotas.

Validate directed matrix, named completeness/gate wiring, five race repetitions,
normal-only rejection, unavailable/GC-active rejection, runner-only omission of
each scheduling hook (then restore source), legacy selectors, both vet modes,
format/whitespace/source hashes. Run actual standard Docker go-all-test last,
including Go short/coverage, mandatory integration, API/OIDC and daemon coverage.
Verify final fixture cleanup, audit, commit/push and create PR without merging.

GREEN closes only measured single-path/file retained-history subset. A genuine
productive retirement RED is required before any minimal runtime fix. W2-10,
E1-10, E1/X1, broader shapes/retention and R31 remain OPEN; preserve RevertFile's
prior disposition. Reevaluate residuals after this slice; do not infer that all
remaining risks belong to Phase5/6 or move them to PRE-GC without evidence.

## Implementation and measured scope

Plan commit 1339cb4c7. Productive root-bound HandleUpload identifies retained
commit/root/FILE_FS1 and ordered canonical SHA-256/external SHA-1 single block.
Only the fixture's upload up: is removed as explicit expiry-state control,
with expiry projection cleanup registered first. Sole inherited permanent fs:
is preserved throughout. No historical metadata/ref deletion manufactures D.

Actual DeleteFile runs on an observed real Cassandra session. For the tagless
fixture, completed empty path-specific tag scan and final library/day decrement
mark both asynchronous paths finished before restore and teardown. Persisted
current root JSON must exist and omit the filename; errors/missing-row fallback
cannot certify absence. Original exact P and bytes are observed independently.

Actual RevertDirents handler receives form-urlencoded commit_id/path and real
owner/org context. This certifies productive handler logic with Cassandra/SILO,
not external routing/auth. Response requires one normalized file success and
empty failed array. Historical hook includes path and FILE_FS1; pre-HEAD hook
includes path. Normal builds call no-ops. No publication behavior is changed.

| Leg | Required observations |
|---|---|
| normal | historical=1, HEAD=1; winning named file retains FILE_FS1/P/K/bytes |
| retained-history-gc | historical=1, HEAD=1; real local positive fs: read settles candidate before claim/global proof/D; no lifecycle/recovery root |
| head-conflict | same positive GC skip; real empty CreateFile wins first CAS; historical=1, HEAD=2, winning parent=competing HEAD, both names remain |

Independent worker observation uses LOCAL_QUORUM positive reference; separate
global liveness readback is observational and is not a worker zero-proof race.
Candidate insertion is controlled discovery for the owned block, not natural
candidate discovery evidence. No candidate preservation/claim-release behavior
is inferred from the early skip. Final ref readback contains sole fs: and no pub:. Writer trace observes no
repair writes/deletion or fs: promotion; it does not monitor every transient
pub: write. Source review confirms this handler has no pub: acquisition path.
Original historical tree and permanent ref are unchanged.

Cleanup ownership is inherited from W2 fixture: early library API deletion,
exact uploaded object/mapping and block/GC artifact cleanup. Own expiry cleanup
is registered before removal. Library/object/mapping/expiry failures report via
testing.T and invalidate child exit; some inherited x1 metadata deletes are
best effort, so this is not a general teardown proof for every legacy test.
Shared-keyspace integration binaries run serially; IDs alone do not isolate
TestMain's stale-library cleanup. No quota enlargement or GC global disabling.

GREEN closes only measured retained-history subset. No retirement RED,
COMMITTED/TERMINAL or general continuity until HEAD is certified. Other shapes,
batch partial-success/quotas, replacement/fallback, concurrent retention,
Phase5/6, R31 and E1/X1 remain OPEN. Prior RevertFile status stays unchanged.

## Validation and audit

Directed final matrix/completeness/gate wiring PASS (10.708s). Five serial
race repetitions PASS (59.314s), exactly 15 named legs, no data races.
Filtered normal-only run rejects missing GC/conflict evidence; unavailable
backend rejects before child tests; authenticated GC-active endpoint rejects
before fixture creation. Runner-only historical-hook omission fails all three
legs (historical=0); pre-HEAD-hook omission fails all three (HEAD=0). These
are scheduling sensitivity controls, not retirement RED or exact-P omission.
Runner restored after both mutations. An initial controls driver stopped on
an incorrect expected wording for backend rejection; the backend correctly
failed, the driver assertion was corrected and the full controls rerun.

Legacy required selectors PASS: exactly original three file restore, three
directory restore and three directory revert legs; no new Dirents test captured.
Normal/integration vet, Docker gofmt and whitespace PASS. Host/restored runner/
final image Go hashes match; frozen plan prefix unchanged. Final actual standard Docker go-all-test PASS, retained container
sesamefs-e110d-final-go-all exited 0: Go short/coverage, required integration
592.182s, API 20/20 suites and OIDC 25/25 tests. New matrix 3/3; prior file
restore, directory restore and directory revert 3/3 each; RevertFile 6/6 and
cross-repo 14/14. All 13 compiled daemon controls PASS, zero GC-disabled skips.
Optional separately tagged gcsoak/multi-DC/cgroup evidence is not certified.

After the complete run, existing cleanup-test-repos.sh on both standard/manual
backends found no owned test repositories to remove; both admin repo lists
are empty. Default organization remains hard/free and existing hard profile
still limits libraries to three. No quota or GC configuration was changed.
Scoped final audit found no unresolved introduced P0/P1/P2. No new publication
protocol or broad content-resurrection closure is claimed. Logs outside Git under
$TEMP/sesamefs-e110d-*.

Go source hashes (host, restored runner and final image checked):

| Source | SHA-256 |
|---|---|
| internal/api/v2/trash.go | 2a8080c33902179d7f5df675de3a25d195c387937f1381181ca7234c3db3d23a |
| internal/api/v2/revertdirents_publication_barriers.go | d5460be968aa2f8bf8f55df081738decb656bc3741774c88cdbb2e31badf598b |
| internal/api/v2/revertdirents_publication_barriers_integration.go | 49bca826445f1b6314ddbd69f9a0e0e7a2a7a9bf678b27168af67b831ea71831 |
| internal/integration/e110d_revertdirents_retained_history_test.go | 30f095fbc405a5e7cb67a66d0a8e2af47ea7281509bdc3b535df8708c2cc68df |
| internal/integration/integration_test.go | 94cb174220928596a861b9a185cfc3ff85c78705fab43e8c48366769c0675abb |
