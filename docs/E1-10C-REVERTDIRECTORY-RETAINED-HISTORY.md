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
