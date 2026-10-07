# E1-12 / W2-12 — definitive loser crash safety

Frozen base: main@2a6eb32fd1a5a6a0e71ddd18ddc0a2fedfaad0eb (#266 merged).

## Contract

Characterize one Office/CreateFile, plaintext single-block attempt on Cassandra/SILO.
Obtain actual HEAD CAS applied=false, stop before request-local cleanup, SIGKILL
its Linux writer process, and restart the productive repair sweep in a new process.
At natural up:/pub: TTL expiry, measure exact P1/K1, HEAD/tree/commit/fs, repair,
refs, candidate, released claim, lifecycle and recovery root. No fabricated repair,
CAS result, physical life, HEAD or D. TTL/repair scheduling control is explicit.

## Sequence and acceptance

1. Normal publication and definitive-loser normal-cleanup controls.
2. Writer pauses before HEAD; an independent empty-file publication wins HEAD.
3. Resume writer to actual applied=false; integration-only observation pauses
   immediately before cleanup. Parent verifies exact attempt, then OS SIGKILL.
4. Confirm durable repair and artifacts survived while competitor HEAD stays exact.
5. Shorten existing real temporary references through the reference API and await
   actual Cassandra expiry. Confirm zero real references and non-expiring repair.
6. Run productive owned-candidate GC before repair can renew pub:. Require direct
   EACH_QUORUM repair guard, no D/root, claim released, candidate/P1/K1 intact.
7. A new process runs the actual bucket sweep, not a direct known-row visit.
   Require native classifier UNKNOWN, retained repair, no fs promotion and
   unchanged competitor HEAD/tree/P1. Repeat recovery to establish stable retention.
8. Guard-only omission mutation must reach exact COMMITTED, finish owned TERMINAL
   recovery and independently verify cleanup. This proves guard necessity, not a
   complete post-D X1 violation.
9. Completeness/unavailable/filtered-child negatives, race repetitions, vet,
   existing W2/G4/G5 gates and standard Docker go-all-test, sequentially.
10. Audit final source/docs/diff and cleanup/quota, commit/push and create PR.

## Disposition and exclusions

GREEN closes only the measured Office definitive-loser crash pre-D safety subset.
W2-12 overall stays OPEN: no durable loser witness or eventual cleanup authority
is supplied. W2-13/14, concurrent cleanup/Paxos, other funnels, Phase5/6, X1,
PRE-GC/A1 and activation stay OPEN. No speculative runtime protocol fix. If actual
runtime produces RED, preserve evidence and fix only the reproduced funnel.
Standard development GC configuration stays unchanged; use existing e19 isolation.
Tests must own and clean all fixtures; quota policy/limits must not be relaxed.

## Evidence

Initial Docker matrix: six legs PASS; productive isolated child 20.48s, parent
24.729s including TestMain teardown. This is initial evidence, not final gates.
Race repetitions, omission, negative controls and full go-all-test still pending.

The clean-loser control lets normal cleanup run and then permits the handler's
ordinary retry to succeed; it checks that the first attempt's repair/pub are gone.
Crash controls kill the real writer after the observed definitive conflict and
before cleanup. Recovery runs the actual bucket sweep in a new test process.
Only created_at/lease_expires_at of the existing real repair are aged/reset for
scheduling; no classification or destructive authority is manufactured. Before
GC, up:/pub: are shortened through AddBlockReference and naturally expire; before
that expiry the test rejects any unexpected permanent reference. These are harness
controls, not a production TTL policy or a time-to-discovery guarantee.

Shared E1-11 helpers execute the exact GC probe and independent owned teardown;
their log labels remain E1-11. Their omission reaches exact COMMITTED and completes
productive TERMINAL recovery before reporting RED. E1-12 does not label that
omission alone as a complete X1 post-D publication counterexample.
## Harness audit corrections

The first omission run failed at an eager guard assertion before productive GC;
that failure is rejected as COMMITTED evidence. The harness now verifies the
actual zero-ref boundary independently, then measures the guard through the real
worker. The corrected omission reached exact COMMITTED, completed productive
TERMINAL recovery and verified absence of owned metadata/repair/K1 (7.561s).
Production guard source is restored by the script trap; no host mutation occurs.

The cleanup control now explicitly observes the original pub:<commitID> before
CAS, and checks its absence after normal loser cleanup. Repair-owned renewal is
separately identified as pub:<repo:commit:fsID>. The normal retry must settle;
fresh-process sweeps must renew that exact repair-owned pub. Final verification
is rerun after these assertion corrections, not inferred from earlier PASS.