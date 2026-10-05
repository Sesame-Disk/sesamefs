# E1-7 / W2-5: Sync RecvFS-before-PutBlock publication continuity

## Frozen plan

Base: main@42dc3c38dc995fb80f74548813648b06395fb7b3, merged #256.

Characterize production before changing it. Split real Sync requests into
PutCommit object, RecvFS, PutBlock and HEAD phases; inspect Cassandra between
completed requests rather than adding runtime barriers for sequential calls.
Use real Cassandra/SILO and the current productive GC worker/authority.

Required named legs: fresh; fresh-head-before-put; reused; reused-committed;
reused-terminal; post-put-gc. Fresh content must show exact SHA-1 metadata,
absent mapping/canonical/P/references before PutBlock, unchanged HEAD, and
safe GC observation without creating missing mappings/blocks/placements.
Reused content is seeded by a productive non-Sync upload; no permanent fs:
is removed to manufacture zero. Observe original exact P and real mapping,
RecvFS replay, no implicit Sync pin, and the actual liveness boundary.
For retirement controls lapse only fixture-owned temporary refs explicitly,
use productive scoped GC to reach exact COMMITTED/TERMINAL, then continue the
real protocol. Assert HEAD/tree/commit, fs:/pub:/up:, repair, canonical and
mapping, lifecycle/root, original K1 and successful replacement P2 if any.
After PutBlock, attempt scoped GC and show convergence to existing readiness,
repair/final exact-P and settlement, retaining the original placement.

If a supported schedule reproduces unsafe retired-P publication, freeze RED
before implementing only the minimum funnel fix and mutation control. Otherwise
make no runtime change. Record CLOSED-EVIDENCE only for the measured ordered
contract; leave wider W2-5/E1-05 obligations explicitly OPEN unless supported
by the complete evidence. PutCommit object persistence is not HEAD publication.
Do not redesign SHA-1 layout, W2-9/10, R31/W2-11..14, Phase 5/6, coordinator,
PRE-GC/A1 or activation. Production GC remains OFF.

Require all named legs through TestMain and both standard Docker commands;
prove required filtered/unavailable runs fail. Execute directed/race evidence,
Docker regression and vet, inspect the complete diff and document precise
limits, then commit, push and create the PR.
