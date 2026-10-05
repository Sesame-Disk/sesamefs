# E1-09 / W2-9 cross-repository publication safety

Date: 2026-10-05. Base: main@975b3057db3e0aefbc09e41e7beeaf0ce68193e7.

## Frozen plan

Characterize current runtime before changing it. One plaintext file, one block,
same organization and block representation, both real AsyncBatchCopy and
AsyncBatchMove -> processSingleItem paths. Copy reads source metadata into a
pending in-memory object; staging persists destination metadata before pub:.

Controls: normal copy/move, retained source fs: during GC, and destination
repair-first protection. Investigate productive source library deletion/purge
and its real worker cascade as an alternative to Phase 5/6. Never delete source
fs: by CQL to manufacture a race. Temporary upload expiry-state controls and
fixture-scoped candidate discovery are explicit controls, not elapsed TTL proof.

Pause after actual source metadata read and before destination pub:. If productive
source cleanup can release all source liveness, drive actual block GC to exact
COMMITTED and separately TERMINAL, then resume. Assert destination HEAD/tree,
exact source/destination fs:/pub:, durable repair, canonical P, lifecycle/root,
K1/bytes, task completion and move source ordering. The RED criterion is durable
reachable destination dependence on retired P1, not endpoint acknowledgement.

GREEN: document only the measured contract. RED: freeze reproducible unchanged
runtime evidence, implement minimal existing exact-P/liveness adapter after
repair and before HEAD, and demonstrate RED -> GREEN and omission sensitivity.
Retries must capture authority per attempt; unknown/error fails closed.

No directories, multi-block/DC, encrypted cross-representation, W2-10,
R31/W2-11..14, Phase 5/6 fixes, coordinator, PRE-GC/A1 or GC activation.

Final validation in Docker: directed characterization, meaningful mutation
control if runtime changes, race repetitions, Go regression/vet and appropriate
standard suite. Audit all changes and claim limits, commit/push and create PR.
## Frozen unchanged-runtime RED

Directed Docker run on production source main@975b3057: integration FAIL
(16.328s). Normal copy/move PASS. Both copy and move publish destination HEAD
and permanent destination fs: after productive source library purge and exact
P1 COMMITTED, and separately TERMINAL/K1 absent. Copy reports task success;
move reports failure later because source library is already gone, but its
destination HEAD/fs: are already published. Task failure is not rollback.

Source deletion used actual owner HTTP soft-delete and permanent-delete routes;
real source-library queue/cascade/processFSObject removed the permanent fs:.
Only source-owned temporary up: was explicitly lapsed. No source fs: CQL delete,
Phase 5/6 scan, synthetic metadata/P/ref/claim or invented GC answer was used.
Completed real source SELECT response was paused before caller continuation.
Real EACH_QUORUM two-zero reads/full repair scan and exact COMMITTED/root,
TERMINAL/K1 deletion checks reuse the prior verified worker helpers.

Raw log: external $TEMP/sesamefs-e19-red.log. The safety assertions fail on this
revision; the next commit must make them pass. Existing HTTP normal copy/delete
regression already exists; the report's "never measured" claim is narrowed to
this previously untested pre-pub purge/retirement schedule.