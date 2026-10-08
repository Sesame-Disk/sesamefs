# E1-15B — writer publication after D, crash before the final fence

Frozen base: main@4290a2bb394b8748973b2a4b7e9b8340d122a2af (#271 merged).

## Question

Can a current Office/CreateFile writer, resumed after exact COMMITTED D(P1),
durably write its original `pub:` (and its durable repair) and then lose the
process before `validateCommitBlockPublicationFences`, which would have
rejected the publication? What does the durable state look like afterwards,
and does the shared repair worker keep that post-D liveness alive?

## Static analysis (main@4290a2bb3)

CreateFile order: materialize P1 + `up:` → `createFileAfterMaterializedBarrier`
→ `prepareFileFSObjectForPublish` → `stagePendingPublishedFiles` (resolve
SHA-1→SHA-256 through `block_id_mappings`, persist the pending fs owner,
`AddPublishAttemptReferences` → `pub:<commit>`) → `fileFromBlocksAfterStagedBarrier`
→ `queuePendingPublishedFileRepairs` (durable, non-expiring R) → `insertCommit`
→ `fileFromBlocksBeforeHeadBarrier` → `validateCommitBlockPublicationFences` →
HEAD CAS. Nothing between materialization and staging checks claim or D.
Mappings survive physical retirement (R11a), so staging should succeed after D.

Expected by code reading, not yet measured:
- crash after staging: a durable `pub:<commit>` after D, TTL 35d, no repair;
- crash after queueing: `pub:` plus a durable R for an attempt that will
  never reach HEAD. The sweep should classify it UNKNOWN and renew
  `pub:<repo:commit:fs>` on every visit, and the #271 post-check would see R
  present and keep the pin. That is post-D liveness renewed indefinitely
  (intersection with `ISSUE-PUBLISH-REPAIR-DEAD-ROW-RETENTION-01`).

## Classification (frozen before runtime, same rule as E1-15A)

RED: any durable `block_references` row for the block written by a current
operation after exact COMMITTED D(P1) and present at the inspection point.
Harm is recorded separately: a TTL-bounded dead pin, or an indefinitely
renewed pin; none of these legs advances HEAD. This PR characterizes and does
not fix. A confirmed RED is registered as one P1 PRE-X1 class ("liveness
written post-D before validation"), linked to the #271 residual and to
DEAD-ROW-RETENTION. The shared fix belongs to a separate design PR.

## Plan

1. Office/CreateFile, real Cassandra/SILO, existing e19 GC-OFF isolation; each
   leg owns an org/library. No production change. No CQL insert/delete of
   references, no harness candidate or repair, no fabricated HEAD/P/D.
2. Legs:
   - `no-gc-control`: in-process writer publishes; fs: and settled repair.
   - `d-before-stage-no-crash`: writer subprocess paused after
     materialization; natural D; resume; the final fence rejects; the
     request fails; pub:/repair cleaned; HEAD unchanged.
   - `d-before-stage-crash-before-queue`: same, then pause at afterStaged and
     SIGKILL (OS signal verified); inspect at EACH_QUORUM from the parent.
   - `d-before-stage-crash-after-queue`: same, pause at beforeHead (after
     queue and insertCommit) and SIGKILL. Inspect pub:, R, commit and HEAD.
     Then run the productive sweep twice (eligibility +2h, then +6h past the
     retry hint; real lease kept ahead of the shared daemon), recording the
     native classification and every reference write.
3. Natural D: the writer's real `up:` is moved by the productive renewal API
   to a seconds-scale deadline and retired by Cassandra TTL; owned-scope Phase
   0 creates the candidate at exact P1; owned-scope Phase 1 and the productive
   worker reach exact COMMITTED (lifecycle PUBLISHED, orphan COMMITTED, root,
   canonical retired). Reuses the E1-15A helper.
4. Teardown: owned root completed to TERMINAL; E1-11/12/13/14/15A verifiers.
   Completeness gate, gate negatives, race repeats, vets, standard
   go-all-test, cleanup/quota checks, scoped audit, PR.

## Out of scope

Repair worker changes, fences, tables/migrations, TTL, Phase 0–6 changes,
Sync/SeafHTTP/OnlyOffice/cross-repo, read-repair/3-DC, scheduler, GC
activation, and any fix of a confirmed RED.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
