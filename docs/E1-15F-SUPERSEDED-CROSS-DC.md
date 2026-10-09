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

### Fix (7f6ba1ee9): unit RED → GREEN

- **RED** on #275 code: `TestSupersededWitnessSurvivesTransientParentReadFailure`
  failed in both variants after the read recovered.
  - genesis: `anchor=h2 exhausted=true`;
  - bounded chunk: `cursor=c-1024` (past P), then exhausted.
- **GREEN:** `go test -race ./internal/api/v2 ./internal/api ./internal/db`
  PASS, including the existing head-observation budget test. A losing
  re-anchor still reads no commit row.
- **Mutations:** `scripts/e115e-superseded-mutation.sh` now has 5 mutations;
  the new `parent-abort` mutation turns the test RED.

### 3-DC evidence (`scripts/e115f-superseded-3dc-validation.sh`)

Two full runs on real three-DC Cassandra, both exit 0, 9/9 phases each. One
further attempt never started because Docker Hub's token endpoint failed
(HTTP 500/504) during `docker build`; that is infrastructure, not evidence.
Phase results:

- **Gate negatives (2/2):** a phase without ids FAILs; an unset phase SKIPs,
  and the runner rejects a SKIP or a missing `E115F_PHASE_DONE` marker.
- **M2** (HEAD = P, sweep from dc-asia): native `unknown/<nil>`. From dc-eu at
  EACH_QUORUM, R is present and R's own `pub:` was renewed.
- **M4a** (dc-eu stopped, sweep from dc-na): `unknown/read parent of repair
  commit … for the superseded witness: Cannot achieve consistency level
  EACH_QUORUM in DC dc-eu`. The visit stopped at the new guard before
  walking. R and its `pub:` are retained.
  - Whole-DC outage also fails the walk's own EACH_QUORUM reads, so #275's
    code would have retained here too. This leg proves fail-closed
    retention; the fix is proven by the unit RED.
- **M4a recovery** (dc-eu restarted, sweep from dc-asia): one visit,
  `superseded/<nil>`, settled.
- **M1**, verified from dc-na and from dc-eu at EACH_QUORUM:
  - R row and R's own `pub:` are gone;
  - staging `pub:<c1>` and the legitimate `fs:<repo:fs>` are kept;
  - SERIAL HEAD = H1; commits c1 and H1 still have parent P.
- **M3:** c2 (parent H1) published by the production SERIAL CAS from dc-eu
  (H1 → c2 → H2). A sweep from dc-na returned native `reachable/<nil>`. From
  dc-asia at EACH_QUORUM, `fs:<repo:fs2>` is promoted, R2 is gone, and R2's
  own `pub:` is gone.

### Standard regression

- Docker go-all-test on 5062b7482, 2026-10-09 16:50–17:26 local: exit 0.
  Integration 1827.132s (76% of 40m); API 20/20; OIDC 25/25. There are 76
  SKIPs: the usual 75 plus the phase-gated 3-DC test, which SKIPs without
  `E115F_3DC_PHASE`.
  - The first attempt failed while building images: Docker Hub's token
    endpoint returned 504. It ran no tests; the retry is the accepted run.
- Afterwards both backends report `CLEANUP_STATUS: clean`, and no 3-DC
  container is left behind.

### Audit

- **Production change:** two early returns plus the memoized read in
  `publish_repair.go`. Any parent-read error other than `ErrNotFound` now
  stops the visit before it walks, at both walk sites (the anchored walk
  and the post-genesis re-anchor walk).
- **No progress is persisted after a failed read.** The cursor is not
  advanced and exhaustion is not marked. A re-anchor CAS that already
  applied leaves a fresh, non-exhausted anchor, which the next visit walks
  with the witness.
- **Trade-off:** REACHABLE detection may be deferred one visit while the
  parent read is unavailable. The walk's reads are also EACH_QUORUM, so it
  would usually fail in the same conditions.
- **Untouched:** Sync, writers, GC, schema and hot paths.
- **Fixtures:** the 3-DC test fabricates no classification, settlement or
  HEAD. Its only fixtures are the library identity rows and `fs_objects`
  content rows.
- No unresolved introduced P0/P1/P2. E1/X1 stay OPEN and GC stays OFF.
  E1-15F is CLOSED-EVIDENCE only for SUPERSEDED in this topology.

