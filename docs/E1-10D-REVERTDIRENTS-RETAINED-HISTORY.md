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
