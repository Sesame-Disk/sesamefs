# W2-0 — publication continuity through HEAD

Base: main@50c50903e7ac49c04ef36f460dccc35ce122dfd6.
Branch: fix/w2-0-publish-liveness-through-head.

## RED before design

Real Cassandra/MinIO TestW2PublicationLivenessThroughHEAD is semantically RED.
T1 (expiry before repair/authority) commits D and returns 409, HEAD unchanged.
T2 (expiry after authority) commits D and returns 201, HEAD advanced depending
on P. Log: tmp/w2-0-red.log. Delete only isolated writer up:/pub: refs to model
TTL expiry; real settled claim, EACH_QUORUM zero-proof, irreversible handoff.

## Baseline chains (before this follow-up)

| Funnel | Order |
|---|---|
| F1 Office | materialize/up → pub → repair → insert commit → exact-P → HEAD |
| F2 UploadFile | materialize/up → Once: pub → repair → insert commit → exact-P → HEAD |
| F3 CFFB | verify/capture P → own up → session claim → shared Once as F2 |
| Sync provenanced direct/merge | pub → provenance/renew/exact-P → repair → tree stats → HEAD |

No current GC gate consults repairs. Existing repair rows have no TTL and carry
canonical block IDs. Their worker retains UNKNOWN/unreachable attempts; known
loser cleanup and successful promotion remove them. No new reaping authority.

## Minimal candidate evaluated after RED

GC EACH_QUORUM zero-proof also scans matching repair rows in existing 32 bucket /
organization clustering ranges. Pin repair writes LQ; consume every page, no
negative cache or age/lease expiry. Read failure blocks D. Re-read refs after a
negative repair scan to cover repair → permanent fs: handoff. Worst cold-path
cost: 32 organization-range queries plus two reference probes; measure it.

Stage → repair gap is REAL: expiry there can let GC win. A repair gate alone
cannot close it. The writer must exact-P validate AFTER acquiring the durable
repair. F1/F2/F3 already do. Sync must keep placements from its provenanced
readiness and validate those same placements again AFTER queueing, never rescope
expired provenance. GC's settled deleting claim precedes zero-proof; it rejects
writers whose repair arrived too late. Pauses after that check are protected by
the repair gate, not TTL. Unprovenanced Sync remains outside this guarantee.

Longer/renewed TTL or standalone advisory checks retain the T2 pause window.
Non-expiring pub: would require new orphan reaping/revocation work in R31. New
schema/coordinator is unnecessary for this candidate. Unresolved crash rows
retain the existing conservative R31 behavior; their cleanup is not redesigned.

## Plan

1. Preserve RED; prove stage-before-repair gap rather than hide it.
2. Implement global repair gate and ref handoff ordering; writer LQ pin.
3. Retain Sync provenanced placements and check them after repair queue.
4. Real T1–T6: before/after authority expiry, normal writer, known loser/retry,
   ambiguous outcome, crash/recovery with existing discovery worker.
5. Verify LQ-write/EACH_QUORUM-read domain and topology, paging/errors/identity.
6. Mutate actual gate/acquisition/release order; require D+HEAD semantic RED,
   fail filtered/unavailable evidence rather than count a skipped test as green.
7. Docker unit/vet/race/full supported integration tests, final scope audit.
8. Only then update X1/R3/known issues/index/current work; audit W2-1/2/6/6a
   individually. Keep W2-0 OPEN unless every requested continuity requirement holds.
9. Commit/push/new PR; no merge or GC activation.

Productive files expected: DB destructive check/repair scan, shared repair writer
consistency, Sync readiness placement retention and final check. No new table,
migration, lease, global LWT, TTL increase or non-expiring references.
Excluded: #234, PC-D1B.5, W2-4/5, W2-7/8/9/10, R31 fixes, Phase 5/6, GC activation.
A shared gate may protect other existing repairs conservatively without closing
those funnels' missing writer authority checks.

## Status

Implementation and Docker audit complete. W2-0 OPEN: partial correction only.

## W2-0 follow-up — durable repair gate (2026-09-29)

The current branch adds a fail-closed GC guard using the existing non-expiring
`published_block_reference_repairs` rows. Acquisition explicitly writes at
LOCAL_QUORUM; destructive readers scan all 32 organization bucket prefixes at
EACH_QUORUM, consume all pages, and repeat the reference probe after a negative
repair scan. Settled GC claims use the existing EACH_QUORUM/global SERIAL domain.
F1 Office, F2 UploadFile and F3 validate exact P after repair acquisition; Sync
now retains its provenanced placements and validates them again after queueing.

W2-0 and W2-6a remain OPEN. The stage-to-repair gap still exists, but the final
check rejects HEAD if GC won there. After acquisition, the non-TTL repair guards
against expiry after validation. Mixed deployments with older destructive
readers do not have this protection. UNKNOWN/unreachable records retain the
existing R31 conservative behavior and can inhibit collection indefinitely.
The cold path adds up to 32 organization range reads plus a final ref probe.

This is a useful partial correction. UNKNOWN settlement is tested through the
existing classifier seam; process death is modeled by an abrupt panic, with
real durable rows and discovery-worker recovery. Neither is a wire-level HEAD
ambiguity or an OS process-kill experiment. Full closure still requires those
experiments and rollout evidence. Historical W2-1/2/6 claims are not upgraded.
See [plan, evidence and limits](./W2-0-PUBLICATION-CONTINUITY.md).

## Global visibility argument and evidence boundary

Repair INSERT explicitly uses `BlockReferenceWriteConsistency = LOCAL_QUORUM`.
For NetworkTopologyStrategy, EACH_QUORUM intersects the acknowledged quorum in
that writer DC regardless of the destructive reader's local DC. A clean global
miss is possible only if no guard was acquired in the scanned bucket, or it was
safely settled. Every page is consumed; iterator errors propagate. UNKNOWN and
unreachable attempts are not age-evicted. After settlement to permanent fs:,
the final EACH_QUORUM ref read observes the LQ promotion preceding guard deletion.
The existing settled deleting-claim proof (EACH_QUORUM + global SERIAL) is reused
when GC wins before acquisition; final advisory exact-P must then reject.

The domain is pinned by `TestW2PublicationGuardReadWriteDomain`; the Cassandra
paging and organization-isolation test exercises real repair queries. This run
uses one DC with RF1. No new three-DC divergence drill was run, and no new empirical
multi-DC claim is made. The per-DC intersection is the existing X2/P4a primitive
argument, now applied to the repair table. Production GC's existing replication
strategy gate remains mandatory.

## Independent re-audit

| Row | New evidence | Status |
|---|---|---|
| W2-1 session CFFB | Own session expiry after final authority; real GC guard; permanent fs settlement | No automatic closure; R31 and full crash/ambiguity evidence remain |
| W2-2 BorrowedFS | Foreign fs removed, own pins expired, GC guard positive, successful fs settlement | No automatic closure; same remaining requirements |
| W2-6 UploadFile | Materialized target captured, own pins expired after final check, GC guard positive | Existing exact-P improvement retained; no full continuity closure claim |
| W2-6a Office | Before repair D commits and HEAD rejects; after final check guard prevents D; loser/retry and aged worker recovery | OPEN: useful continuity correction, incomplete full closure evidence |

No schema, new coordinator, lease, TTL or GC activation change is included.
Older readers must be upgraded before relying on this guard. A stranded existing
repair may prevent a new GC handoff indefinitely; this conservative availability
cost is intentional pending R31, and is not a newly invented reaping policy.
## Docker evidence (previous f6d93e3 audit)

- Pre-fix semantic RED: `tmp/w2-0-red.log`, real Cassandra/MinIO,
  `expiryAfterAuthority`: D committed and HEAD advanced.
- Directed baseline: all 15 named legs GREEN in the mutation script baseline.
- Actual mechanism mutations: omit GC gate, omit repair acquisition, release
  repair before HEAD. All three are semantic D+HEAD RED, not compilation failures.
- Evidence controls: filtered legs and unavailable backend both fail closed.
- `go test -short -count=1 ./...`: GREEN.
- `go vet ./...`: GREEN.
- `go test -race -short -count=1 ./internal/db ./internal/api ./internal/api/v2 ./internal/gc`:
  GREEN (db 57.042s, api 18.428s, api/v2 4.324s, gc 4.734s).
- Complete supported Compose integration profile: GREEN, including all 15 mandatory continuity legs. Optional three-DC/cgroup drills remain skipped by their existing environment gates.

Logs: `tmp/w2-0-mutations.log`, `tmp/w2-0-unit-vet-race-final.log`,
`tmp/w2-0-integration-full.log`. Scripts use disposable Docker copies; no mutant
is applied to the host. All three backend images were rebuilt for the complete
profile. The only later source edits are formatting and contract comments.

Measured on the local single-DC RF1 fixture: a repair-only positive probe took
4.8–44.3ms; 32 empty organization ranges plus final ref probe took 49.9ms.
These are test timings, not production capacity claims or a bounded cost:
organization repair backlog makes scan work proportional to pending rows.

Final profile: tmp/w2-0-integration-final.log, exit 0. The first profile found
a literal evidence-gate source mismatch (fixed) and a concurrent-upload 403.
Both passed isolated recheck; the final full profile ran without concurrent
mutation cleanup and passed. Do not run these integration runners concurrently
against one shared fixture: TestMain cleanup is process-local. GC is restored
to false on all three local backends after the audit.

## PR #239 crossed audit corrections (2026-09-30)

Both reported P1s are confirmed on f6d93e3 and corrected.

| Decision | Before new D | After D COMMITTED |
|---|---|---|
| REAL_REF | Exact release; existing candidate settlement | Existing real-reference contradiction policy |
| REPAIR_GUARD_ONLY | Exact release; preserve candidate, discovery and queue; postpone without retry | Cannot veto or revoke D |
| ZERO | May prepare and commit exact D | Continue existing committed authority |
| UNKNOWN/error | Fail closed; ownership rules govern release/queue | Preserve existing fail-closed behavior |

The mandatory GCStore pre-handoff primitive is now
`BlockPublicationLivenessGlobal`; `BlockHasReferencesGlobal` again reports
actual `block_references` only. The 32 bucket/page scan, LQ acquisition,
EACH_QUORUM reads and final real-reference handoff probe remain intact.
No TTL, schema, coordinator or repair-cleanup authority changes.

Actual Cassandra/MinIO worker regressions are mandatory evidence (17 named legs):

- Repair before D: candidate and discovery survive, exact claim releases,
  retries stay zero; removing the repair lets the next pass commit and retire.
- Sync passes initial readiness; exact PREPARED recovery state and D are
  committed before repair acquisition; final captured-P check rejects HEAD.
  Late repair remains present while the actual worker resumes that same D and
  retires canonical metadata. The discovery recovery pass retains COMMITTED
  continuation, its published lifecycle and exact physical bytes.

Scope boundary: this branch deliberately lacks the future physical executor.
These tests prove G2/G3 continuation and retained physical authority; they do
not claim terminal physical DELETE. PC-D1B.5/#234 remain separate. Existing
R31 UNKNOWN/abandoned repairs can block a NEW D indefinitely; they cannot
newly veto an already committed D. W2-0 and W2-6a remain OPEN; GC stays disabled.

Semantic pre-fix evidence: `tmp/w2-p1-worker-red.log`. Corrected directed
evidence: `tmp/w2-p1-green.log`. The mutation script adds actual worker
candidate-consumption and post-D repair-veto mutations to the three existing
D+HEAD mutations; incomplete/unavailable evidence must fail closed.


### Integration cleanup settlement

The first full run failed the existing scanner trigger with GC disabled.
With the supported local scanner enabled, every test passed, but TestMain's
final verifier found 17 orphan admin projection rows after the single cleanup
snapshot. Canonical library retirement can occur after that snapshot.

The test harness now retries its existing orphan-only cleanup within the
existing verification timeout and requires the same zero-orphan invariant.
It does not skip verification, ignore rows, change runtime cleanup or add
new reaping authority. Two focused tests prove late-orphan settlement and
failure on persistent corruption/cleanup outage. Real Cassandra worker
regressions and cleanup recovery passed: `tmp/w2-p1-cleanup-recovery.log`.


### Final Docker audit results (2026-09-30)

- `go test -short -count=1 ./...`: PASS.
- `go vet ./...`: PASS.
- Race detector for db/api/api-v2/gc: PASS.
- Final directed baseline: all 17 mandatory legs PASS.
- Five mechanism mutations: semantic RED (three D+HEAD, two actual worker
  candidate-consumption/late-repair-veto failures); both evidence gates fail closed.
- Focused cleanup settlement/persistent-failure controls: PASS.
- Full supported Compose integration profile: PASS, exit 0; integration package
  309.372s, including all mandatory evidence and the unchanged zero-orphan check.
  Optional three-DC, process-kill and cgroup drills remain environment gated.
- GC_ENABLED=false restored and inspected on all three local backends.

Final logs: `tmp/w2-p1-unit-vet-race.log`,
`tmp/w2-p1-unit-vet-final.log`, `tmp/w2-p1-contract-final.log`,
`tmp/w2-p1-mutations-final.log`, `tmp/w2-p1-cleanup-recovery.log`,
`tmp/w2-p1-integration-final-settled.log`.
The failed configuration/cleanup runs are retained; they are not counted as
GREEN. Integration runners were serialized against the shared fixture.

Final scope audit: both confirmed P1 regressions corrected; no further merge
blocker found in this partial correction. PC-0 now also records Sync's captured-P
check after repair acquisition. No schema, new coordinator, lease, TTL, repair
revocation authority or GC activation change. W2-0/W2-6a/X1 remain OPEN, with
physical executor and full crash/ambiguity/rollout proof explicitly pending.
