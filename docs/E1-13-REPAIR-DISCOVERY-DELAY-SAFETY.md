# E1-13 / W2-13 — pending repair safety during discovery delay

Frozen base: main@fc50e168db91212cdb6d74b78f3f7db8756c6310 (#268 merged).

## Contract and plan

Measure one Office/CreateFile plaintext single-block durable repair not yet visited
by the repair sweep. Target and backlog repairs must all come from productive
writers interrupted before HEAD, followed by real independent competitor HEADs.
No fabricated repair, HEAD, P, classifier, references, D or promotion.

1. Five named legs: control-no-backlog, backlog-before-target,
   target-not-yet-visited-gc, fresh-process-backlog, eventual-target-visit.
2. Observe actual bucket coordinates; choose naturally earlier buckets for real
   backlog. created_at/lease controls change eligibility, never ordering. No
   substituted listing, sorting inside the production sweep or relocated rows.
3. Integration-only exact-session/library observer before a productive visit;
   pause earlier work while target has not entered repair execution. Distinguish
   bucket listing, visit entry and native classification.
4. Shorten only existing real temporary references and await Cassandra expiry;
   check zero global refs, non-expiring target repair and exact HEAD/tree/P1/K1.
5. During backlog pause run real owned-candidate GC; observe EQ repair guard,
   released claim, retained candidate, no D/root, exact P1/K1 intact.
6. Resume productive sweep; verify observed backlog order then native UNKNOWN,
   exact repair-owned pub renewal, no fs promotion, unchanged target HEAD/tree/P.
   Fresh process restart must rediscover Cassandra rows without local hints.
7. Omit only GC pending-repair lookup in a disposable test container. Same
   unvisited-target zero-ref schedule must reach exact productive COMMITTED;
   finish owned TERMINAL and independently verify all fixture cleanup.
8. Independent metadata/bytes/expiry tracker+projection cleanup for every fixture,
   completeness/unavailable/filter negatives, race repeats, vet, existing gates,
   standard Docker go-all-test, final source/docs audit and quota checks.
9. Commit/push and create reviewable PR. No merge or GC activation.

## Disposition limits

Only measured Office delayed-discovery current-version pre-D safety can become
CLOSED-EVIDENCE. W2-13 overall remains OPEN P2 discovery/convergence/scale. Finite
backlog and eventual visit are not arbitrary-delay proof, fairness, SLA or bounded
completion. GC's direct EQ scan also has backlog/availability cost; failures must
remain fail-closed. W2-12/14, concurrent cleanup, other funnels, Phase5/6, E1/X1,
PRE-GC/A1 and activation remain OPEN. No worker redesign, new index/schema,
parallelism, health gate, bucket-count/TTL or standard dev GC configuration change.

## Validation

Pending implementation and productive evidence. No result is inferred from plan.

## Harness and initial evidence

The first productive matrix passes all five legs: isolated child 28.68s, parent
34.773s including TestMain cleanup. Final strengthened-source checks are pending.
A diagnostic command initially inherited unrelated mandatory gates from Compose;
that filtered command correctly failed completeness and is not accepted evidence.
Dedicated commands explicitly unset unrelated gates; standard go-all-test retains
all mandatory gates.

Every target and backlog repair comes from real Office/CreateFile execution paused
by the existing interruption seam after exact-P authority, before HEAD. This is
an in-process abrupt-interruption fixture, not an OS writer crash claim. A separate
productive blockless competitor advances HEAD, yielding native UNKNOWN later.
Each fixture owns its org/library; the worker backlog spans orgs. This measures
sequential repair discovery delay, not same-org GC scan scalability or large load.

Rows keep their productive bucket and PK. The harness observes natural hash
placement, creates up to 16 owned fixtures until three earlier-bucket repairs
exist, and chooses a target in the later bucket. Sorting selects fixtures only;
production listing/order is untouched. Equal-bucket extras remain under their
original scheduling lease and are still independently cleaned. Age/lease changes
are explicit eligibility controls, never an ordering or latency guarantee.

The before-visit observer is scoped to exact DB session/library and runs after
scheduling filters, before repair execution; normal builds compile it as a no-op.
The child pauses at its first backlog entry. At that point target has not entered
repair execution; absence of durable target anchor/cursor/exhaustion and zero
refs independently detect out-of-band visitors. Child accounting separately
requires each selected row's actual native UNKNOWN classifier result. Resume must
visit every selected backlog before target and renew its exact repair-owned pub:
without fs promotion, HEAD/root/P changes, repair removal, D or recovery root.

The fresh-process leg SIGKILLs this paused sweep and verifies the OS signal. A new
process must rediscover the same first backlog from Cassandra and eventually
visit target; no local scheduling map is seeded, cleared or copied. This does not
characterize sustained duplicate workers or a populated local-backoff restart.

Shared E1-11 GC/teardown helpers retain their E1-11 log labels; auxiliary-expiry
cleanup retains E1-12 labels. Cleanup covers every created fixture, including
unselected extras, through exact metadata/repair/K1 and tracker/projection checks.
The omission must reach productive COMMITTED then own TERMINAL before teardown;
it proves guard necessity, not a post-D publication violation.

## Cleanup audit correction

A read of a completed E1-13 fixture found one pending_published_fs_objects owner
and its discovery projection after metadata/repair/K1/expiry teardown. Existing
shared fixture cleanup did not cover that separate surface. This introduced P2
TEST-INFRA is fixed within E1-13: enumerate actual owned-library fs coordinates
before metadata teardown, read exact owners, use DeletePendingPublishedFSObjectOwner,
and independently require canonical and exact by-day projection absence afterward.
The omission script requires the affirmative owner cleanup marker and rejects
owner read/delete/identity/verification errors. No publication/runtime change.

Prior completed E1-13 fixtures are repaired only at coordinates from their own
successful teardown logs, after verifying library/block metadata absent and exact
owner org identity. 89 owned fixtures were checked and 89 owners plus their exact
projections removed and verified. No active or foreign fixture is purged; quota
policy and limits are unchanged. This does not repair unrelated historical tests.

## Pre-owner-cleanup verification (historical source)

- Strengthened target-not-visited assertions: 15/15 race legs across three runs
  PASS, 129.501s; ordinary/integration vets, completeness and inventory PASS.
- Three E1-13 negative controls PASS. Guard omission reached productive exact
  COMMITTED while target unvisited, completed own TERMINAL, metadata/K1/expiry
  teardown PASS (11.708s); it predates independent pending-owner cleanup.
- Four W2, three E1-11 and two G5 negative controls PASS. A shell command used an
  incorrect W2 script filename after successful own gates/mutation; the existing
  gates were subsequently run with the correct filename and exit 0.
- Earlier Go file SHA-256:
  95096721dc25f5995bac67b55d2fe156aafe4b51b4fc3cbe0e9311b1252d168e.
- First standard Docker go-all-test exit 0: integration 668.529s, API 20/20
  and OIDC 25/25. This is historical, before pending-owner correction.
  Corrected-source race/gates/mutation/full regression and final audit are pending.

## Safety disposition

The measured Office single-block pre-D subset has positive guard evidence during
controlled backlog delay. The target repair exists before claim/proof and cannot
expire; GC directly scans its org prefix at EQ, independent of worker discovery.
Native UNKNOWN settlement retains it and does not promote fs or publish HEAD.
Removing only that destructive guard permits exact D. This supports only the
measured pre-D subset; W2-13 overall stays OPEN P2 discovery/convergence/scale.

Neither finite timing nor selected bucket order proves arbitrary delays, SLA,
fairness, sustained load, same-org GC scan scalability, outages, concurrent cleanup,
progress Paxos races, post-COMMITTED revocation or other funnels. The direct GC
scan also has backlog and availability cost; read errors remain fail-closed under
the existing protocol. No scheduler/TTL/index/health-gate or dev GC change.

## Backlog selector audit correction

The first selector checked an incorrect sorted index: some schedules obtained
only two strictly earlier-bucket repairs despite the three-row minimum in the
plan. The measured delay was real, but those runs do not certify that minimum.
Selection now requires the third-lowest bucket to precede target and independently
asserts at least four actual eligible coordinates (three backlog + target).
This changes fixture selection only, never the productive scan or row placement.

The intermediate owner-cleanup source passed 15/15 race legs in 149.424s; it is
historical for the final backlog-size contract. Final source must repeat the
matrix, negative controls, omission and standard regression with both corrections.
