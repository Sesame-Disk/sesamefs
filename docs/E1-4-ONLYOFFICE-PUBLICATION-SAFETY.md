# E1-4 / W2-8: OnlyOffice callback publication safety

## Frozen plan

Base: main@d2cb034dd3a33b9d91a652432fc6dad429863804.

Use the real EditorCallback (download HTTP, document-key lookup, materialization,
publication, settlement) against Docker Cassandra and pinned SILO. First run the
current production behavior. No authority/liveness results may be fabricated.

Required independent legs:
- Normal save: callback error=0, reachable HEAD/tree, exact permanent fs:, bytes,
  repair/pending-operation settlement, document-key behavior.
- After materialization and before durable publication repair, remove only the
  operation's temporary references through the production API as an explicit
  expiry-state control; real GC commits D1(P1), then callback resumes.
- Same boundary, but real retirement reaches TERMINAL and K1 is absent before
  callback resumes.

Observe actual placement P1/class/key, HEAD/commit/tree, up:/pub:/fs:, durable
repair, OnlyOffice pending operation, lifecycle/root, K1, callback JSON error and
replay. HTTP 200 alone is not successful OnlyOffice publication. A replay may
materialize P2, but may not revive P1. Do not delete durable repairs to force D.

If current behavior is RED, fix only this funnel by carrying the original
materialized placement through repair acquisition and invoking the existing
exact-P authority validator immediately before HEAD, with owned cleanup on
rejection. Repeat evidence on fixed behavior and a targeted omission control.
If GREEN, add no runtime guard.

Run relevant checks in Docker, including real-service directed tests, race
repetitions and affected unit tests; broaden to the standard full suite for a
runtime change. Record source provenance and any skipped evidence.

Close only the measured pre-HEAD contract. W2-8/E1-08 post-HEAD cleanup/recovery,
R31 W2-11..14, other funnels and X1 remain OPEN unless separately demonstrated.
No schema, TTL policy, scheduler, SeafHTTP, copy/move, resurrection, Phase 5/6,
PRE-GC or A1 changes. Production GC remains OFF; isolated test worker control
is evidence, not activation authorization.

## Execution

The plan was committed as ad151bdf4 before the harness and fix. Current runtime
on main@d2cb034dd plus an integration-only observation barrier was tested first.
The production build implements that barrier as an empty function; the
integration implementation affects only the named in-process repository and
changes no DB/storage outcomes. Tests run sequentially.

| Leg | Current behavior | Fixed behavior |
|---|---|---|
| normal force-save, then close-save | GREEN: error=0, HEAD/file/block reachable, exact fs:, repair removed, pending operation cleared | GREEN, including reuse of the same physical placement |
| P1 COMMITTED before callback continues | RED: error=0, HEAD advanced, fs: installed while D1 held P1 | error=1, HEAD unchanged, no fs:, pub:/late repair cleaned, pending operation cleared; replay rejects while D1 continues; success after concurrent recovery requires exact D1 TERMINAL plus new P2 proof |
| P1 TERMINAL/K1 absent before callback continues | RED: error=0, HEAD advanced and fs: installed with canonical P1 and K1 absent | error=1, HEAD unchanged, no fs:, owned publication state cleaned; close-save replay materializes P2 at a different key, publishes exact reachable fs:, K1 remains absent |

Baseline behavior failures were both `E1-4 VIOLATION: callback published retired
P1`, with `error=0` and `fs=true`. The normal control passed. Setup failures
(maximum document size omitted, and a legacy orphan path that lacked the G2
PREPARED-to-COMMITTED promotion) were corrected before counting the final RED
run. They are not runtime counterexamples.

The final harness executes exact claim -> real global zero-proof ->
PrepareBlockDeleteOrphan -> CommitBlockDeleteOrphanHandoff ->
PromoteBlockDeleteOrphan, and observes the exact COMMITTED orphan and recovery
root before resuming. The TERMINAL leg additionally finalizes canonical P1 and
uses the real fixture-scoped recovery worker to finish DeleteExact(K1), lifecycle
termination and root/orphan cleanup. The real OnlyOffice pending-cleanup row already exists in this window;
it does not supply the durable publication-repair guard. No repair is removed
to enable D. Candidate
scanner/queue scheduling is not exercised by this directed pre-D store sequence.

The callback is the registered productive EditorCallback, including JWT
verification, real document-key and write-permission lookups, real HTTP download
and the shared materialization/publication/settlement path. An HTTP fixture
serves unique bytes in place of an editor session; running the OnlyOffice editor
UI is not claimed. Primary races use status 6 (force-save); normal and terminal
replays use status 2 (close-save). A successful close removes the document key;
a repeated close with that absent key returns error=1 without downloading or
changing HEAD. This existing behavior is observed, not changed to a no-op
success policy.

The writer observer acknowledges a real durable repair INSERT. Normal settlement
observes fs: INSERT before repair DELETE; rejected attempts leave no fs: or pub:
and no owned durable repair. The actual OnlyOffice pending operation is matched
to its up:<operation>, repo, block and storage class before the race. Cleanup
removes only this fixture's resources.

## Runtime fix

Carry the originally confirmed SHA-256/class/key into
publishEditedDocumentMetadata, including its HEAD retry loop. After durable
repair and pending-commit recording, invoke the existing
validateCommitBlockPublicationFences / ValidateBorrowedFSPublicationAuthority
immediately before UpdateLibraryHeadFromSnapshot. On rejection, use existing
owned OnlyOffice publication cleanup. The outer callback retains its existing
JSON error protocol and pending-operation cleanup. No new GC authority primitive,
TTL, schema, materialization policy or repair scheduler is introduced.

## Validation and limits

Directed fixed legs pass against Docker Cassandra 5.0.9, single DC RF1, and
pinned SILO RELEASE.2026-09-16T00-00-00Z (MinIO-compatible S3).
The first full integration run passed all behavior tests but failed the existing
evidence-gate wiring guard: the new flag was missing from the TestMain startup
requireEvidence chain. That wiring is corrected and the guard passes.
Omitting only the new final exact-P call inside the runner reproduces both
behavior failures, while the normal control passes. The original runtime file
is restored and byte-compared before further validation. A filtered run of only
the normal leg is rejected by the named-leg gate (missing=committed,terminal).

The first ten race repetitions exposed a harness assumption: a real background
recovery worker finished D1 during a committed-leg replay, and the callback
legitimately materialized P2. Nine repetitions passed; one failed the old
always-reject replay assertion. The final assertion accepts success only with
an exact old D1 TERMINAL certificate (claim/time/class/key), absent D1
orphan/root and K1, a distinct P2 key, actual bytes and reachable HEAD/fs:.
Primary post-D P1 publication rejection remains unchanged. The final ten race
repetitions pass in 89.104s with no race reports.

The final standard Docker Go unit/integration + API + OIDC command passes at
the final harness source (integration 505.294s), with all required evidence
gates including OnlyOffice. Both normal and integration-tagged go vet pass.
Final whole-diff/format checks pass; service tests were sequential in the same
runner. The rebuilt go-all-test image contains these exact Go source hashes.

Source hashes for runtime/test/TestMain match host, runner and rebuilt image:
- onlyoffice.go: 57deab1b655917e0e5dc9d0d7029716bc0a8eca04dd9e5bef0d308910588bba7
- e14_onlyoffice_test.go: a0c57c25fe1ef5969a5862e3548632197b5824d898126accfc09c5ed1980805d
- integration_test.go: 67337248a72a73699bd54fc25e41d658d5ba891a01019d4000cafd2f6111ebd0

The new named-leg gate SESAMEFS_REQUIRE_E14_ONLYOFFICE_EVIDENCE rejects filtered
or skipped evidence; standard Docker integration/all-test commands require it.
Temporary up: removal is explicit expiry-state control, not a measured 48h TTL.
The cases use unencrypted libraries; encrypted callback publication and
cross-class reuse are not separately exercised.
No multi-DC failure, live editor UI, OS restart, concurrent owner cleanup,
post-HEAD delayed repair, W2-11..14 or natural candidate-discovery closure is
claimed. CLOSED-FIX applies only to the measured pre-HEAD contract. The broader
W2-8/E1-08 row, common R31 and X1 remain OPEN; production GC remains OFF.
