# E1-10b / RestoreTrashItem directory retained-history

## Frozen plan

Base: main@2f5635621f1a0dafafd24e0916fcdf88e8d0f149 (#259 merged).
Measure one historical directory with one child file, one plaintext block,
same repo/org, retained commit/fs_objects, pre-HEAD and one DC. No runtime fix
before a supported retirement RED.

Build the tree productively using CreateDirectory and HandleUpload into that
parent, then DeleteDirectory. Observe historical commit/root/DIR1/FILE_FS1,
canonical SHA-256/external SHA-1 layout, exact P/K/bytes and sole permanent
fs:<repo>:<FILE_FS1>. Explicit lapse of only the own upload up: is an expiry-
state control, not elapsed TTL. Register owned expiry cleanup before removal.
Never remove historical fs: or invent D. Synchronize observed asynchronous
DeleteDirectory work before restore/teardown; do not sleep to assume completion.

Required independent legs: directory-normal, directory-retained-history-gc,
directory-head-conflict. Reuse #259 hooks without adding production code.
Pause after oldEntry == DIR1; run the actual block worker with exact fixture-
controlled candidate discovery and productive liveness/settlement queries.
Require positive local historical fs:, candidate settled, no global zero-proof,
claim/D/recovery root, original P/K/bytes. Resume and verify full current HEAD
root -> named DIR1 -> named FILE_FS1 -> canonical block -> exact P/K/bytes.
Force a real competing HEAD before first CAS; require one historical hook, two
HEAD attempts, winning commit parent == competitor and both entries intact.

Use a separate gate and test prefix that does not match #259's ^TestE110 selector.
Reuse manual-GC sesamefs-e19/sesamefs_e19 through a dedicated child process;
required child completion and subtest filters remain mandatory, all other gates
stay in the parent. Leave #259's three-leg contract/helper unchanged.

If historical retention blocks D, CLOSED-EVIDENCE only for the measured subset;
COMMITTED/TERMINAL remain unexecuted. If productive D and retired-P1 publication
are reproduced, freeze RED and implement only the minimal necessary funnel fix.
No directories deeper than one level, general multiblock, retention cleanup,
Phase 5/6, R31/W2-11..14, multi-DC, #258 follow-ups or activation.
RevertFile evidence is not reclassified; W2-10/E1-10 overall and E1/X1 stay OPEN.
Production GC OFF; normal dev daemon behavior unchanged.

Run directed matrix, race repetitions, filtered/unavailable/active-daemon and
historical-hook omission rejection controls, both vet modes and final Docker
go-all-test. Audit source, cleanup, gate independence and claim limits; commit,
push and create PR. Update CURRENT_WORK, E1 ledger, KNOWN_ISSUES, CHANGELOG and
X1-CRITICAL-PATH without closing W2-10 or declaring all residual risk Phase 5/6.
