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
