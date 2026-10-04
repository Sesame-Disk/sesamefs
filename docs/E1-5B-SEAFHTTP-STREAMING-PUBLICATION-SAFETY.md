# E1-5b / W2-7b: SeafHTTP streaming publication safety

## Frozen plan

Base: main@8ce5fd8c70acf8709d7518daee1634f2bddf3b5d.

Characterize productive chunked HandleUpload (multipart + Content-Range,
Cassandra token/permission checks and pinned SILO) before runtime changes.
Use at least two distinct internal blocks (8 MiB block boundary).
Capture every original internal SHA-256/storage-class/storage-key after
confirmed registration and before publication repair. Observe exact up:, pub:,
fs:, HEAD/commit/tree, pending owner, repair, D lifecycle/root and physical key.
Hooks only schedule/observe; never fabricate query outcomes.

Required real-service legs: normal multi-block; victim COMMITTED; victim
TERMINAL/key absent; TERMINAL same-tracker retry legitimately rematerializes
only the victim to a different P while preserving valid blocks; real HEAD CAS
conflict validates every original placement again. Remove only the victim's
owned up: as explicit expiry-state control; never remove repair to force D.
Freeze baseline RED before production fix. Omission of only the new gate must
restore retirement REDs while normal remains GREEN. Required evidence gate
rejects missing named legs or unavailable infrastructure.

If RED, retain placements atomically with per-position accounting, restore
original placements on retries, pass them through every metadata HEAD attempt,
and validate all distinct internal blocks with the existing publication
authority primitive after durable repair, immediately before HEAD. Missing,
contradictory, malformed or unknown placement/authority fails closed.
Blocked/Changed preserves retryable 409 and identifies rejected block IDs.
Clean only owned failed-attempt state. Invalidate all positions of rejected
block IDs, then rerun productive materialization on same-tracker retry;
never replace cached P by merely reading a new canonical tuple. Preserve
valid positions. Bound validation concurrency, preserve SHA-1 wire identities.

Cover duplicate coherent/contradictory positions, missing placements and
Unknown authority, cleanup and retry identity in meaningful tests. Run Docker
unit/integration checks, race repetitions, omission and incomplete-run gates;
audit final source and evidence before commit/push/PR.

Closure is only measured streaming pre-HEAD safety/retry recovery. Single-shot
behavior, post-HEAD/R31, W2-9/10, W2-11..14, natural TTL/candidate discovery,
process restart/distributed tracker, multi-DC certification, Phase 5/6,
PRE-GC/A1 and GC activation are excluded. E1/X1 remain open. Production GC OFF.
