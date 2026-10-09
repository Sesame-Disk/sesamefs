# E1-15F — SUPERSEDED settlement across datacenters

Frozen base: main@e7a83ea789e8 (#275 merged).

## Purpose

#275 added SUPERSEDED, the first negative outcome that authorizes removing
durable protection, and validated it single-DC only. E1-15F validates it on
real three-DC Cassandra (dc-na, dc-eu, dc-asia; NetworkTopologyStrategy, RF=1
per DC). There, the SERIAL HEAD needs 2 of 3 DCs and EACH_QUORUM needs all
three.

## Defect found in review (THIS slice, P2 liveness, safe direction)

If the EACH_QUORUM read of the target's parent fails but the walk succeeds,
#275 disables the witness for that visit and keeps walking:

- the walk passes P unrecognised and reaches genesis: the anchor is marked
  exhausted, and every later visit returns UNKNOWN while
  `liveHEAD == exhaustedAnchor`;
- or a bounded chunk persists a cursor beyond P.

Either way R stays retained until HEAD moves again, which in an idle library
may be never. A throwaway unit probe on main reproduced it: visit 1 failed
the parent read, `exhausted=true`; visit 2, with the read working again,
returned UNKNOWN and deleted nothing.

Minimal fix (`publish_repair.go` only): when the parent read fails with any
error other than `gocql.ErrNotFound`, the visit returns UNKNOWN with that
error **before walking**, persisting no progress. The walk's own reads are
EACH_QUORUM too, so nothing reachable is lost. The next visit walks with the
witness. `ErrNotFound` (no commit row) keeps the current behaviour.

Residual, documented and not fixed: a commit row that is missing at a visit
and appears later (a writer stalled more than 5 minutes between repair
queueing and `insertCommit`, then crashing). It is rare, and it fails toward
retention.

## Evidence rules for the 3-DC harness

The 3-DC harness has no HTTP backend on the 3-DC cluster: tests run
in-process with one session per DC. Rules:

- **CQL fixture only for content and identity rows:** the `libraries` /
  `libraries_by_id` rows (plain representation), and `fs_objects` rows for
  the M3 promotion.
- **Production code for everything else:**
  - commits through `db.AuthorizeCommitProjection` +
    `db.MaterializeAuthorizedCommit`, the production commit path;
  - HEAD moves through `FSHelper.UpdateLibraryHeadFromSnapshot` (the SERIAL
    CAS);
  - repairs through `QueuePublishedFSObjectBlockReferenceRepair`;
  - staging `pub:` and legitimate `fs:` through the production
    block-reference functions.
- **Never fabricated:** classification, settlement, HEAD outcomes or GC
  results. Classification and settlement come only from the production sweep
  (`RunPublishedBlockReferenceRepairSweepAt`), observed by the existing
  after-classify hook.
- **Cross-DC verification reads at EACH_QUORUM.** Settlement deletes at the
  default LOCAL_QUORUM, and a LOCAL_QUORUM read in another DC can lag. That
  lag is not a defect: GC's destructive repair scan reads at EACH_QUORUM.

## Matrix (one script, each phase from a named DC)

| Leg | Setup | PASS |
| --- | --- | --- |
| M2 | c1 (parent P) written from dc-na; R queued; HEAD = P; sweep from dc-asia | native UNKNOWN; R kept; R's own `pub:` renewed; verified from dc-eu |
| M4a | one DC (dc-eu) stopped; HEAD advanced to H1 (child of P) beforehand from dc-eu; sweep from dc-na | UNKNOWN with an availability error; R and its `pub:` kept |
| M4a-recovery | DC restarted; sweep from dc-asia | native SUPERSEDED, settled |
| M1 | verified from dc-na and dc-eu at EACH_QUORUM | R row and R's own `pub:` gone; staging `pub:<c1>`, legitimate `fs:<repo:fs>`, HEAD = H1, commits c1/P/H1 unchanged |
| M3 | c2 (parent H1) with R2; HEAD advanced to c2 then H2 from dc-eu; sweep from dc-na | native REACHABLE, never SUPERSEDED; R2 settled by promotion (`fs:` added, R2 gone) |

M4b, a failure of only the parent read while the walk works, cannot be
induced deterministically on real Cassandra without new network tooling. It
is covered by the unit RED→GREEN.

Gates: the script requires every PASS line. A completeness check fails on
SKIP or a filtered run. Negative controls cover the gates.

## Out of scope

Repairs without a commit row, a cancellation protocol, Sync, schema and
migrations, GC Phase 0–6, GC activation, worker optimisation, re-running the
historical 3-DC matrix, and E1/X1 closure. E1/X1 stay OPEN; GC stays OFF.

## Validation

Accepted results are recorded below. Nothing is inferred from this plan.
