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