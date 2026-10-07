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
