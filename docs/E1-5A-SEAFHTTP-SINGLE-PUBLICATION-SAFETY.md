# E1-5a / W2-7a: SeafHTTP single-shot publication safety

## Frozen plan

Base: main@8319d077b512cd9bbd0e3028bf7fd2d26fd3dbb5.

Exercise productive HandleUpload (HTTP multipart, token, materialization,
publication and settlement) against Docker Cassandra and pinned SILO. Commit
this plan before changes; characterize current runtime before any authority fix.

Observe original internal SHA-256 of stored bytes, exact storage class/key P1,
owned up: identity, HEAD/commit/tree, pub:/fs:, durable repair, pending owner,
D lifecycle/root and physical K1. Pause after confirmed registration and before
publication repair. Hooks may observe/control scheduling, never fabricate
liveness or authority results.

Required legs: normal success; exact D1 COMMITTED before resume; exact D1
TERMINAL with canonical P1 and K1 absent before resume. Remove only the request's
own up: through the production reference API as explicit expiry-state control.
Do not remove repair to manufacture D. The request must not publish HEAD/fs:
depending on retired P1. Replays may create P2 only with its independent valid
physical life and unchanged old retirement certificate.

Only if current behavior is RED, carry original materialized exact P unchanged
through commitUploadedFile and every commitUploadedFileOnce retry. Acquire
existing durable repair, then use db.ValidateBorrowedFSPublicationAuthority
immediately before HEAD. Fail closed on errors or non-Authorized outcomes;
use existing attempt cleanup without modifying or revoking D. Preserve external
SHA-1 protocol identities and error/response contracts.

Add HEAD-conflict retry evidence proving the original P is validated on each
attempt, and an omission mutation removing only the new authority check:
normal stays GREEN, COMMITTED/TERMINAL return RED. Required-evidence gate must
reject missing named legs and unavailable infrastructure. Validate directed
real-service tests, race repetitions, affected tests/vet and full Docker suites.
Audit the final diff, source provenance and documented closure before PR.

Close only supported single-shot pre-HEAD evidence. Streaming/multi-block,
post-HEAD/R31, W2-11..14, X1 remain OPEN. No ChunkUpload state, copy/move,
resurrection, schema, TTL policy, repair scheduler, natural candidate discovery,
Phase 5/6, PRE-GC or A1 changes. Production GC remains OFF; test retirement is
an isolated proof, not activation authorization.

## Execution and measured contract

The plan was committed as 13e7e6e69 before the characterization hook or fix.
Baseline main@8319d077 plus only the integration observation barrier produced:
normal PASS; COMMITTED and TERMINAL RED at `E1-5a VIOLATION: upload published
retired P1`. Both returned 200, advanced HEAD and installed permanent fs: after
the exact P1 retirement boundary. The terminal leg first proved canonical P1
absent and K1 deleted by productive recovery. No publication repair was deleted.

The fix carries stored-byte SHA-256/class/key from confirmed HandleUpload
registration into each metadata attempt. After pub:, durable repair, pending
owner and commit materialization, it uses the existing public exact-P authority
validator immediately before HEAD. Blocked/Changed map to the existing retryable
409 deletion contract; errors and every other non-Authorized outcome fail closed.
Existing attempt cleanup deletes the losing commit and owned pub:/repair/pending
owner. Shared content-addressed metadata is retained, as the existing cleanup
requires. D authority is never modified by the rejection path.

| Leg | Productive evidence |
|---|---|
| normal | HTTP 200 with external plaintext SHA-1, reachable HEAD/tree/file, exact fs:, original P1 and correct K1 bytes; no pub:, repair or pending owner; acknowledged fs: INSERT before repair DELETE; upload-token replay retains live P1 and the external SHA-1 response (autorename is observed in production logs, not separately asserted). |
| committed | Own exact up: removed via the reference API as expiry-state control; real global zero-proof, claim, Prepare/CommitHandoff/Promote establish exact unrevoked D1 and COMMITTED orphan/root with K1 present before resume. Resume returns 409 without HEAD/fs:P1; owned attempt commit/pub:/repair/pending owner are gone. Replay rejects or succeeds only if real recovery completed D1 and a new live P2 is proven. |
| terminal | Same pre-publication boundary, then real canonical retirement and fixture-scoped recovery delete K1 and settle D1 TERMINAL. Resume returns 409, HEAD unchanged, no fs:, no attempt commit/pub:/repair/pending owner, P1/K1 absent. Replay materializes P2 with a different key; old exact TERMINAL D1, absent old root/orphan and absent K1 remain proved. |
| head-conflict | After the first actual exact-P read, a real empty-file CreateFile wins HEAD. The real CAS loses, owned cleanup acknowledges repair DELETE, retry creates a second repair and executes a second LOCAL_QUORUM authority SELECT using the original P tuple. The winning commit parents the competitor HEAD; fs: settlement follows, and both pending owners/repairs are gone. |

Only the first three legs characterize retirement. The conflict control proves
propagation and repeated validation with original live P; it does not claim a
separate retirement interleaving between metadata retries.

A Cassandra query observer records actual repair INSERTs, authority SELECTs and
fs:/repair DELETE acknowledgements without substituting results. The before-HEAD
hook reports the original tuple; the retry additionally requires a productive
query acknowledgement per attempt. All hooks are repository-scoped and empty in
normal builds. HTTP, Cassandra token lookup, permission checks, materialization,
HEAD and settlement execute the production SeafHTTP path.

Omitting only `validateSeafHTTPSinglePublicationPlacement` in the isolated
container makes COMMITTED and TERMINAL RED again (200, reachable HEAD, fs:P1),
while normal stays GREEN. The host source is never mutated; the original
container source is restored and byte-compared. The mandatory gate
`SESAMEFS_REQUIRE_E15A_SEAFHTTP_SINGLE_EVIDENCE=1` requires all four named legs
and participates in TestMain infrastructure readiness and both standard Docker
integration commands. Running only normal fails with
`missing=committed,terminal,head-conflict`.

## Limits and disposition

CLOSED-FIX applies only to measured SeafHTTP single-shot pre-HEAD retirement
safety. E1-07 remains PARTIAL and W2-7 streaming/post-HEAD/R31 remain OPEN.
W2-11..14 and X1 remain OPEN; production GC remains OFF. No ChunkUpload state or
streaming finalizer changed.

Fixture topology is single-DC Cassandra (RF=1) and pinned SILO, with unencrypted
libraries and default storage class. This does not certify encrypted uploads,
cross-class reuse, multi-DC availability, concurrent owner cleanup, process
restart, post-HEAD repair, elapsed TTL, candidate discovery, or full scanner
scheduling. Retirement uses real store authority calls after productive global
zero-proof; physical terminal deletion uses the productive recovery worker.
The COMMITTED observation is at the pause: background recovery may legally
advance D1 afterward, but its exact certificate may never be revoked or reused.

## Audit correction from full-suite validation

The first full-suite attempt exposed a pre-existing E1-4 harness assumption:
`e14_onlyoffice_test.go:274` re-read the canonical P1 row after callback rejection
and failed `not found` when the real background recovery retired it. This is
legal progress of the already committed D, not post-D publication. Replace only
that post-rejection observation with the exact old D1 class/key/claim/timestamp
certificate, allowing its published lifecycle row to advance to terminal. Both
E1-4 and E1-5a still require rejection/unchanged HEAD/no fs:P1 and independently
prove the original COMMITTED boundary before resume. Terminal successful replay
still requires absent K1/root/orphan and live new P2. OnlyOffice runtime is unchanged.

The post-rejection certificate check also requires the original deleting claim
and committed handoff when a single global read still observes canonical P1.
Canonical absence or a distinct P2 is allowed as legitimate recovery progress.

## Final validation and provenance

All checks ran in Docker against Cassandra 5.0.9 (single DC, RF=1) and
`pgsty/silo:RELEASE.2026-09-16T00-00-00Z`. The standard go-all-test command
completed Go short/coverage, required integration, API and OIDC successfully.
After the final test-only certificate assertion was strengthened, the complete
required integration suite was repeated on final Go source: PASS, 581.561s.
Final checks then ran sequentially: ten race repetitions of both E1-4 and E1-5a
(30 Office + 40 SeafHTTP named cases), PASS in 247.185s without race reports;
exact-P omission and filtered evidence controls PASS; normal and integration-tagged
`go vet ./...` PASS. The omission's intentional RED results are not suite failures.
Diff whitespace and gofmt checks pass. Final review found no pending P0/P1/P2
within this PR's measured scope; broader open issues above are unchanged.

The new runtime executes through in-process productive handlers in the directed
integration tests. Existing dev backend services were not restarted/deployed;
broad API/OIDC results are checks against that running dev stack, not deployment
validation. Optional isolated multi-DC and saturation/clock drills are not claimed.

Final SHA-256 source provenance (host and validation container agree):

| File | SHA-256 |
|---|---|
| internal/api/seafhttp.go | 4e2292af4dda1d384e35d016c5f2a911220d5f9a965e3cf1e0ca07027999db7b |
| internal/api/seafhttp_publication_barriers.go | 79060ae031fc36df224029859b6fb68b3ec9d06b27bc0a6ba6979203a567e20a |
| internal/api/seafhttp_publication_barriers_integration.go | b341b3ebd5e268d04329e0db47a5b8af3dc73f5ac252284cb8ec29b501547961 |
| internal/integration/e15a_seafhttp_single_test.go | 124476b2efd00cfd8c0667a3b2d34a60599ebd6390ed7e72cdffe2f969f33000 |
| internal/integration/e14_onlyoffice_test.go | 00f981ef5e17953ec2bff4ca4bb3abf5859481dc6347db175389767fb2164c0f |
| internal/integration/integration_test.go | 07de8af9144a240a9d120efa7aeab753bd800cd7eb352a1d9b853ca1f86b48c5 |

Logs are preserved outside Git in the task's temporary `sesamefs-e15a-evidence`
directory: baseline RED, first full-suite failure, successful full suite, final
integration, race, omission, filtered-gate and vet results. To repeat the normal
required checks, build and run the standard Docker `go-all-test` service. A
focused run must clear other `SESAMEFS_REQUIRE_*` gates, enable the desired E1
flags and select all their named legs; selecting only normal must fail the gate.
