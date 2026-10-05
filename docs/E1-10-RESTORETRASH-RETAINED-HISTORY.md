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

## Measured outcome

Frozen plan commit: b823f11de. The three-leg retained-history schedule is GREEN
on current production behavior. No fence, borrowed adapter or repair protocol
was added. Only two scheduling calls with non-integration no-op implementations
were added to RestoreTrashItem.

| Leg | Observation |
|---|---|
| normal | Productive upload/DeleteFile/RestoreTrashItem returns 200 success; winning HEAD reaches the original historical fs_id; exact P/K/bytes unchanged |
| retained-history-gc | After oldEntry capture, the real worker reads the sole historical fs: at LOCAL_QUORUM; settles the controlled candidate before claim/global proof; no D/root; restore succeeds |
| head-conflict | Same retained-history GC attempt; real CreateFile wins HEAD before first restore CAS; actual restore retries; winning commit parents that competing HEAD and preserves both entries |

The fixture uses productive SeafHTTP HandleUpload with token/permission lookup,
real Cassandra and SILO. Delete and restore execute actual handlers in-process
with the actual library owner and real permission checks. This is not evidence
of external restore routing/authentication. Commit/root/tree/layout are observed
before deletion, at the actual historical hook and after restore. Original
canonical SHA-256 and external SHA-1 block IDs, size and exact (class,key) remain
unchanged. The writer never captures P itself; the harness observes P separately.

Exactly one upload-owned temporary up: is explicitly removed as an expiry-state
control; historical fs: is never removed. It is the sole remaining reference
before, during and after publication. No elapsed-TTL behavior is inferred.
Candidate/queue discovery is controlled with the existing exact fixture helper;
actual worker queries, liveness, claim and settlement behavior are unchanged.
The observer requires one positive local reference read, no global worker proof
or repair-domain scan, and candidate absence after processing. Independent
EACH_QUORUM RealReference readback is not a worker zero-proof. Canonical SERIAL
readback, lifecycle lookup and recovery-root discovery show no destructive
handoff, while SILO confirms original bytes. No claim-release or postponed-
candidate behavior is inferred from this early exit.

Restore creates no repair row or permanent fs: settlement writes; the permanent
reference is inherited, not newly installed. The head-conflict control requires
one historical read and two actual HEAD attempts, the competing commit as the
winning parent, and both files reachable. Existing fixture cleanup owns the
unique random block, physical key, refs/mapping and library. The new test also
registers explicit cleanup of each removed temporary pin's expiry projection;
no shared data or global GC settings are altered.

## Disposition and limits

RestoreTrashItem measured retained-history single-file/one-block/plaintext
schedule: CLOSED-EVIDENCE only for the positive observations above. E1-10 and
W2-10 overall remain OPEN. Existing RevertFile evidence is not reclassified.
RevertDirectory, RevertDirents and broader RestoreTrashItem remain unmeasured.
This result does not guarantee that every existing commit has a permanent pin.

COMMITTED/TERMINAL retirement legs are UNEXECUTED: this supported retained state
stops the worker before claim/D. Removing fs: alone would manufacture the deciding
precondition. A legitimate source-publication or history/cleanup interleaving is
needed to exercise another state. Concurrent retention removal, Phase 5 shared
fs cascade, Phase 6 execution TOCTOU, directory/multiblock/multi-DC, post-HEAD and
R31 are neither exercised nor closed. W2-11..14, #258 source-identity/fan-out
follow-ups, E1 and X1 remain OPEN. Production GC remains OFF; normal development
GC inheritance and the separate E1-09 keyspace remain unchanged.

## Validation

Directed three-leg matrix and independent named-leg contract: PASS (18.865s).
Final-source race, rejection controls and full regression results are recorded
below. The initial directed run alone is not the final regression certificate.

### Harness correction and isolation

The first active-daemon five-count race run FAILED (97.910s): one required
owned-worker attempt found zero real queue rows after the background daemon
consumed its candidate. This is missing harness evidence, not a retired-P1 RED;
it is retained as a failure. The standard daemon is never disabled to fix it.
E1-10 now reuses the existing sesamefs-e19 backend/sesamefs_e19 keyspace through
SESAMEFS_E110_ISOLATED_URL. It adds no services/schema/TTL/runtime protocol.
Both standard suites dispatch the same test binary to an isolated child, retaining
-race instrumentation. Only its three-leg gate is required in that child; all
other mandatory gates remain required in the normal parent/keyspace. The child
must finish every named leg before parent evidence is recorded. Subtest filters
are preserved, so a normal-only parent run cannot silently certify all three.
Authenticated GC status must explicitly report enabled=false for every endpoint
configured in that evidence process. The normal dev daemon cannot scan this
keyspace. Direct runs against active GC are rejected before fixture creation.
E1-09 remains unchanged and runs sequentially in its own matrix.

Five final isolated -race repetitions: PASS (83.639s), 15 named legs. Parent and
child top-level PASS lines must not be counted as extra measured legs.
Required normal-only filter: expected rejection with two child legs missing;
parent also rejects incomplete isolated evidence. Required unavailable backend:
expected rejection before tests. Active-daemon endpoint: expected rejection at
the authenticated status check before fixtures. Remove only the historical hook
call inside the isolated runner: expected failure in all three legs with actual
historical visits=0; HEAD-conflict still retries. This is scheduling sensitivity,
not exact-P-gate omission or a retirement RED. Runner source restored afterward.

Initial runner/build timing and unquoted dotted binary-flag invocations produced
setup/argument failures, not evidence; corrected runs below supersede them.
No weakened assertions, accepted peer proof or speculative production fix.

### Final-source checks

Final directed isolated matrix plus independent named-leg/TestMain wiring checks:
PASS (15.833s). Normal and integration go vet: PASS. Docker gofmt and git diff
whitespace checks: PASS. Host, rebuilt go-all-test image and restored validation
runner agree on these Go hashes:

| Source | SHA-256 |
|---|---|
| internal/api/v2/trash.go | 2cee833e77e35575a4f0c8690b50a072fb0d5394a2dae15f76ffd3e0afb94ab9 |
| internal/api/v2/restoretrash_publication_barriers.go | a7a7cd7d0353f7afbaa080e2eef59de4000d55e049eb4e37a6856bfe102d2b7d |
| internal/api/v2/restoretrash_publication_barriers_integration.go | 19efcde62197881ea5addf3b531afb8ddfeb03a4983a6bc4af5f27fd22d804be |
| internal/integration/e110_restoretrash_retained_history_test.go | 2298adbd7e0233d3b1f64b71526416ad4d9f873e3babd30a7df566c4070561ec |
| internal/integration/integration_test.go | 30bab72932823621e2b5d1a98d3673a2a40d189e74f98e797f7b82f5702f4909 |

A runner-only cleanup of the 18 unique early fixtures removed exactly their 18
remaining provisional expiry rows and by-day projections after confirming each
canonical block was absent. No shared/live block was removed. Final test cleanup
registers each owned projection before lapsing its up: row. Logs remain outside
Git under $TEMP/sesamefs-e110-*.log. No new backend image is deployed by this
slice; current handlers compile in the test runner against real Cassandra/SILO.
External regressions use the existing dev backend fleet.

Actual final command: docker compose --profile test run --rm go-all-test.
PASS, exit 0: Go short/coverage; required integration 862.293s; API 20/20 suites
(106s); OIDC 25/25 checks, zero OIDC skips. E1-10 3/3 and E1-09 14/14 named legs
PASS. All 13 requireGCEnabled cases plus the share-link scanner projection test
PASS (14/14 daemon-dependent controls); zero GC-disabled skips. Native HEAD
ambiguity 104.66s and process-kill continuity 60.97s PASS. Optional multi-DC and
cgroup evidence is not certified; separate gcsoak build-tag runs are not claimed.

Final scoped audit covers the two scheduling boundaries, exact historical
commit/root/tree/layout and inherited reference identity, real worker early exit,
candidate settlement, no lifecycle/root/claim, P/K/bytes, real HEAD conflict,
cleanup ownership, child mandatory gate and filter preservation, GC isolation,
source hashes and documented scope. No unresolved introduced P0/P1/P2 was found.
Existing W2-10 P1, retention/Phase 5/6 and R31/general X1 risks remain OPEN.
No runtime migration, new fence, full W2-10 safety, E1 PASS or activation claim.
