# E1-10 / RestoreTrashItem retained-history characterization

## Frozen plan

Base: main@53298f7af4a1efc51039291146adc5f6e1204c43 (merged #258).

Measure only real RestoreTrashItem single-file, one block, plaintext, same
repo/org, retained historical commit and fs_object, pre-HEAD, single-DC.
Start with productive upload and DeleteFile. Observe historical fs_id, ordered
canonical/SHA-1 layout, original exact P, permanent fs:, HEAD/commit/tree,
repair rows and physical bytes. Pause the actual restore after oldEntry capture.
Only this fixture's temporary upload pin may be explicitly lapsed as an expiry-
state control; do not claim elapsed TTL. Never remove historical fs: or invent D.

Required legs: normal, retained-history-gc, head-conflict. Drive the real GC
worker with exact fixture-scoped candidate discovery. Observe the positive local
reference read, candidate settlement, no claim/D/recovery root, and original P/K.
An independent global read is observation, not a worker zero-proof. Resume the
real restore and assert historical fs_id is reachable and bytes unchanged.
Force a real competing HEAD before the first CAS; prove retry preserves both
entries and parents the winning commit to the competing HEAD.

If this supported state reaches COMMITTED/TERMINAL and restore publishes retired
P1, freeze RED before implementing the minimum funnel-specific fix. Otherwise
retain production behavior and close evidence only for the measured schedule.
COMMITTED/TERMINAL legs are unexecuted unless productively reachable.

Add integration-only scheduling hooks and a mandatory named-leg TestMain gate in
both Docker suites. Required filtered/unavailable runs must fail. Run directed,
-race repetitions, scheduling-hook omission control, vet and final go-all-test in
Docker. Audit source, cleanup and claim limits; commit/push/create PR.

No directories, multi-block, history expiration, Phase 5/6, multi-DC, post-HEAD,
W2-11..14/R31, #258 follow-up fixes, scheduler/coordinator or activation changes.
W2-10, E1 and X1 remain OPEN; production GC remains OFF. Shared dev GC behavior
and E1-09 isolated keyspace stay unchanged. RevertFile's earlier evidence is not
reclassified as a general closure.
