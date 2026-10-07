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
At this initial snapshot race repetitions, omission, negative controls and full go-all-test were still pending. Final verification below supersedes that status.

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
## Positive exclusion proof and current disposition

For this actual failed Office attempt the competitor HEAD/root stays exact and
does not publish the losing file. The writer has terminated, so its request-local
cleanup cannot race our recovery. `UpdateLibraryHead` returns the definitive
conflict on a successful MapScanCAS response with applied=false; the observation
is reached only after that conflict, never by substituting the CAS result.

The non-expiring row contains this exact canonical block and persists after all
real refs expire. `BlockPublicationLivenessGlobal` reads real refs globally and
then directly scans the pending repair domain at EACH_QUORUM. A matching row is
REPAIR_GUARD_ONLY, which releases the held claim and preserves the candidate;
the productive probe observes that exact path. Omission of only this scan reaches
exact COMMITTED with matching P/class/key/claim/orphan/root: the guard is necessary.

A new repair process runs the native resumable classifier through the productive
bucket sweep. Exhausting the current HEAD ancestry remains UNKNOWN because there
is no durable negative witness. UNKNOWN renews its own pub: and retains the row;
settlement does not promote fs: or write HEAD. Two new processes demonstrate both
initial discovery and stable post-progress retention. Lease resets are scheduling
controls, not a discovery SLA or additional cleanup authority.

Disposition: the measured Office definitive-loser crash pre-D safety subset is
CLOSED-EVIDENCE. W2-12 overall remains OPEN for durable loser classification,
cleanup/convergence and retention debt; this PR intentionally supplies no witness.
The omission is guard necessity evidence, not a complete post-D X1 violation.
No production protocol/query/TTL/health-gate change; only the observational Office
hook, compiled as a no-op outside integration. Other funnels, concurrent cleanup,
W2-13/14, Phase5/6, E1/X1, PRE-GC/A1 and activation stay OPEN.

## Pre-expiry-cleanup verification (historical snapshot 6cdca06e1)

- Docker race: 18/18 measured legs across three independent child runs PASS,
  89.498s including parent/child teardown. Completeness and gate inventory PASS.
- Ordinary and integration vets PASS on the audited source.
- Three E1-12 filtered/unavailable/filtered-child negative controls PASS.
- Guard omission: exact productive COMMITTED, own TERMINAL recovery and
  independently verified cleanup PASS (7.413s); source restored afterward.
- Four existing W2 closure negatives PASS. Existing E1-11 three negatives and G5
  two negatives PASS in the preceding sequential validation.
- E1-12 file SHA-256 in the full-test image matches the audited working tree:
  9f92c052da8466596d2289c53db13def3efbffb4c4be02021ffa34d14f73d6a5.
- Standard Docker go-all-test PASS on that snapshot: integration 593.530s, API 20/20 suites and OIDC 25/25. This precedes the auxiliary-expiry cleanup fix and is not the final corrected-source run.

Every counted leg records evidence after the independent owned teardown. That
check verifies blocks/refs/lifecycle/orphans, commits/fs/index/repair and exact K1
absence; normal fixtures use their own org and do not consume shared user quota.
No concurrent integration job or stale cleanup is run during go-all-test.
Source audit: all five changed Go files in the running full-suite container match
the audited host bytes. The dedicated race/gate container differs only in gofmt
formatting/trailing newline of the two barrier files; their canonical gofmt
output matches the host exactly. No executable source difference is hidden by the
image reuse. Shared standard development GC configuration is unchanged by diff.
## Final cleanup audit correction

A direct post-teardown Cassandra read found one gc_provisional_block_refs row
for a completed crash fixture, with its non-expiring by-day projection. The child
writer cannot populate its parent's request-local uploadRefs, so the existing
fixture helper omitted this auxiliary cleanup. This introduced P2 TEST-INFRA is
corrected locally: teardown enumerates only the owned org/block tracker partition,
uses DeleteProvisionalBlockReferenceExpiry for every exact recorded coordinate,
and an independent later check requires both canonical and exact by-day rows absent.
The omission script additionally requires that affirmative expiry-cleanup marker
and rejects its failures. Production protocol and shared cleanup remain unchanged.

The preceding 593.530s full-suite PASS did not check these auxiliary rows, so it
is historical rather than sufficient final evidence. Final race/gates/mutation
and full Docker go-all-test are repeated on the corrected source. Prior local runs'
owned residues are also removed through the productive helper, only for org/repo
coordinates logged by E1-12 completed teardown and with blocks/library metadata
independently absent. 36 canonical trackers and matching projections were removed;
81 finished fixture coordinates were checked. No active fixture or foreign row is
purged, and quota policies/limits are not changed.