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

Pending. Results will be recorded after current-behavior characterization.
