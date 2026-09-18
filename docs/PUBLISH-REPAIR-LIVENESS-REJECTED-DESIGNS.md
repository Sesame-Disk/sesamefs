# Publish-repair liveness — rejected designs (#220, #222) and the design gate for the next attempt

**Status:** decision record. Documentation only.
**Issue:** `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` — **OPEN**, P1, PRE-X1 / PRE-GC.
**Parent:** `d6936323b` (`main` containing #221).
**Branch:** `docs/r31-publish-repair-liveness-lessons`.
**Rejected, closed without merge:** PR #220 (`fix/r31-publish-repair-renew-before-classify`, last head `21ad59719`, closed 2026-09-17) and PR #222 (`fix/r31-renew-before-classify-minimal`, last head `eeb2eba7e`, closed 2026-09-18).
**Not this PR:** runtime Go, CQL, migrations, tables, GC, `PublicationCoordinator`, repair worker, witnesses, leases, generations, retries, re-fencing, cherry-picks from #220/#222, X1/W2/R31 closure.

This file is the source of record for why two runtime attempts at this issue
were rejected, for how the normal case actually works in `main` (§8), and
for what any next attempt must prove **before** production code is written. Other documents carry status, scope, a short summary and a
link here; they do not repeat this postmortem.

> **Nothing from #220 or #222 is in `main`.** `main` has no `:walk` referrer,
> no `WalkLivenessDeadline`, no `EACH_QUORUM` absence decider for the
> repair-owned pin, no walk-pin fan-out deadline, and none of the mutation
> legs M1–M25 those branches added. Everything below that describes those
> mechanisms describes **rejected prototypes**, kept as evidence of required
> properties, not current behavior.

---

## The two invariants this work established

```text
1. MAIN-LIVENESS NON-REGRESSION

   If main would keep a block continuously live under an admissible
   schedule, the replacement must not create a zero-reference interval.

2. BOUNDED DURABLE STATE UNDER UNLIMITED RETRIES

   Any durable state introduced by retries must remain structurally
   bounded. If correctness depends on reclaiming, deleting or reusing
   that state, the exact ownership/coverage must be proven before doing
   so. TTL-bounded over-retention may be explicitly accepted when it
   cannot create under-retention.
```

A future design that cannot demonstrate both does not pass from design
review to implementation.

---

## 0. Where this sits (PR #201 / X1 / W2 / R31)

PR #201 froze the X1 physical-life handoff architecture
([GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md](./GC-X1-PHYSICAL-LIFE-HANDOFF-PLAN.md)).
This issue is one residual inside it:

```text
X1
└── W2 / R31 — full publication continuity (up → pub → HEAD → fs)
    └── post-HEAD publish repair liveness
        └── ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01
```

It is neither X1 as a whole nor "the `PublicationCoordinator`". It lives in
the **durable repair that runs after a publication is already visible**:

```text
HEAD visible
→ durable repair row pending (published_block_reference_repairs)
→ a worker visit must resolve reachability
→ while it does, the temporary pub: liveness of the staged blocks can expire
```

Consequences that stay true after this record:

```text
X1 remains OPEN
W2 / R31 remain OPEN for this residual (most of R31-A/C1 is closed on its narrow terms)
GC remains OFF (gc.enabled: false on every replica in every DC)
```

Do not read this as "all of W2 is broken": convergence under a moving HEAD
(#219, `ISSUE-PUBLISH-REPAIR-REACHABILITY-CONVERGENCE-01`) is closed; this is
the liveness handoff residual. Do not read it either as "closing the
`PublicationCoordinator` removes this problem": crash/recovery after HEAD
still needs a correct liveness contract of its own.

---

## 1. The problem, stated without any solution

`main`'s visit today (`repairPublishedBlockReferenceRepair`,
`internal/api/v2/publish_repair.go`):

```text
hydrate the durable repair row
→ bounded reachability classifier
     (SERIAL HEAD read, up to 30 s of EACH_QUORUM parent reads,
      progress LWTs: anchor / cursor / exhaustion / re-anchor)
→ REACHABLE  : promote fs: → remove the repair-owned pub: → delete the row
→ otherwise  : if the row is still pending, renew the repair-owned
               pub:<repo:commit:fsID> (35 d TTL) and retain the row
```

The classifier consumes a meaningful amount of time. If the prior reference
is near its TTL:

```text
prior pub: valid
→ classifier runs
→ prior pub: expires during the classifier
→ the durable renewal happens afterwards
```

there is a **zero-reference interval** between the expiry and the renewal.
Recreating `pub:` afterwards does not remove the interval that already
existed; once GC is destructive, that interval can become a delete. That is
`ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01`.

Two neighbouring problems are explicitly **not** this issue and were out of
scope for both attempts:

- a visit that starts **after** all prior liveness already expired cannot be
  protected retroactively (`ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01`,
  `ISSUE-GC-PUB-REF-ZERO-REF-01`);
- the 35-day over-retention race between a renewal and a concurrent clear
  (`ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01`).

Two facts about `main` that both audits relied on, and that the next design
must carry as constraints:

- the retry hint (`nextRetryAt`, cap `publishedBlockReferenceRepairRetryMax`
  = 6 h) is **process-local** and is not block liveness; a queued repair row
  is not a reference;
- the post-write gone-check of the renewal
  (`renewPublishedBlockReferenceRepairLivenessIfPending`) decides absence on
  the session-consistency read (`LOCAL_QUORUM`) and then removes the
  repair-owned pin: destructive authority is local. #222 prototyped an
  `EACH_QUORUM` decider for it; that prototype is not in `main`. This is an
  independent defect of `main`, tracked as its own issue —
  `ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01` — because it survives
  whatever shape the next renewal design takes.

---

## 2. PR #220 — first rejected attempt

### 2.1 Initial idea: move the durable renewal ahead of the classifier

```text
main:   hydrate → classify → renew 35 d
#220:   hydrate → renew 35 d → classify
```

The intuition is correct and survives: **an operation that consumes a
meaningful share of a TTL must not run on liveness that can disappear
underneath it.**

### 2.2 What it broke

Writing the **durable 35-day** pin before the classifier widened `main`'s
crash window. A writer's ordinary settlement deletes only the row:

```text
visit writes the 35 d pub:
→ a concurrent writer settles / clears the repair row
→ the visit crashes before compensating
→ repair owner gone, 35 d pub: remains, nobody will remove it
```

Even a visit that would have been immediately REACHABLE in `main` (the common
case) can now leave a 35-day ownerless pin. That is over-retention, not
under-retention, but it is a **new** window introduced by the change, and
the audit of that shape asked for it to be recoverable.

**What #220 did not establish.** It did not show that *accepting* that
residual is invalid. The ownerless pin is TTL-bounded (35 d), written under
a stable per-row identity (refreshed in place, so it does not accumulate
across visits), and cannot cause under-retention; `main` already tolerates
a 35-day over-retention residual of the same kind
(`ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01`). The option "renew the
durable pin before the classifier and explicitly accept and bound the
over-retention" was never evaluated on its own terms — the work went into
reclaiming the pin perfectly instead, and that is what grew into §2.3. That
option is neither adopted nor excluded here; it goes through the design
gate of §6 like any other.

### 2.3 The durable cleanup witness

To make that ownerless pin recoverable, #220 grew a durable protocol: a
`published_repair_liveness_cleanups` witness table, a per-visit
`producer_token` in the physical referrer, PREPARING / ARMED / FREEZE
exact-lease Paxos CAS, pins written `USING TIMESTAMP = lease`, CONSUMED
witnesses re-fenced at `EACH_QUORUM` every 3 days for the pin TTL, a
mandatory final fence before a SERIAL terminal delete, `gc_grace` pinned by a
migration. The protocol audited clean on its own terms (no P0/P1 in the
runtime). **That was not why it failed.**

### 2.4 The property that killed #220

A repair may remain pending for an **unlimited** number of visits (persistent
UNKNOWN, crashes, CAS losses). If every visit needs new durable
state / recovery ownership:

```text
unlimited retries → potentially unlimited durable producer state
```

Requirement, literally, for any future design:

```text
unlimited logical retries
+ structurally bounded durable producer state
+ no producer/state may be discarded unless safe ownership/coverage
  has been proven
```

The bound must hold even when visits crash, remain partial, lose CAS, fail
their cleanup, or the repair stays UNKNOWN forever. "Normally it gets cleaned
up" is not a bound.

### 2.5 Bounding attempts that failed audit (do not re-propose without a new proof)

| Attempt | Why rejected | Rule |
| --- | --- | --- |
| **Lifetime producer budget** — allow only N producer generations | Bounds storage by exhausting retries; violates unlimited logical retries | retries are not a resource to spend for storage |
| **Greatest-lease / newest-witness compaction** — drop older producers because a newer / higher-lease one exists | The newer producer may have written only a **subset** of the blocks; order does not prove coverage | ordering between producers is not coverage proof |
| **Same-token reuse without a durable generation** — reuse the physical identity once the previous producer is "gone" | ABA: a sweep that decided Gone for generation N can retire/fence generation N+1 under the same identity; equal or inverted leases cross generations | an old generation may never physically fence or retire a newer one |
| **Complete-only reuse** — reuse only fully completed witnesses | Partial producers, CAS losers and crashed producers still accumulate | partial state is the case that needs the bound |

### 2.6 Other lessons from #220 that stay mandatory

- **Local absence is never destructive authority.** A `LOCAL_QUORUM` read that
  does not see the repair row cannot authorize removing liveness. A
  destructive multi-DC absence decision needs global-enough authority
  (`EACH_QUORUM` was the right authority for the currently certified
  topology, one replica per DC), and an unavailable DC must **fail closed**.
- **Partial completion is not coverage.** FINISHED / newer / greater lease
  does not mean every exact staged block is protected. A coverage proof must
  bind the exact generation and the exact block set.
- **Liveness written before owner stability may outlive its owner; decide
  what that means.** Either explicitly accept and bound the resulting TTL
  over-retention (it must not be able to cause under-retention), or provide
  a durable recovery path if stronger cleanup is required. Same-process
  `defer`, process-local retry and best-effort compensation must never be
  the only argument when correctness depends on the cleanup.

---

## 3. PR #222 — second rejected attempt

### 3.1 Idea: a transient, distinct, write-only pre-pass

Instead of writing the durable 35 d pin before the classifier, #222 wrote a
separate transient referrer before it:

```text
pub:<repo:commit:fsID>:walk   TTL 1 h
```

with the properties: distinct from the durable repair pin (the short TTL can
never land on the 35 d identity — the db helper derived the `:walk` referrer
itself), stable per repair row (refreshed in place, never one per visit),
write-only (never DELETEd, never compensated, expires by TTL), no schema, no
durable per-visit state. The crash residual became a short self-expiring
reference instead of a 35-day ownerless pin. That was a real conceptual
improvement over #220.

### 3.2 Corrections made along the way that are lessons, not current behavior

Each of these was found by audit on `6574b760d` / `96b3afd08` / `c8b241722`
and fixed by `eeb2eba7e`. They are correct requirements for any future
pre-step and must not be lost — but **they exist only in the closed branch**.

**(a) A failed or partial pre-step must not bypass main's retention path.**
An earlier shape did `partial :walk write failure → return`, i.e. no
classifier and no durable renewal, with the ordinary retry up to 6 h away
while the partial walk pins last 1 h. The corrected shape:

```text
pre-protection failure
→ skip the classifier
→ StillPending → ordinary durable renewal → retain with the original error
```

Rule: *failure of an added pre-step must not skip the liveness-retention
path `main` would have reached.*

**(b) A time budget must bound execution, not measure it afterwards.**
An earlier shape ran the fan-out, then checked `elapsed > 20 min`. That does
not stop a fan-out that already took 70 minutes. The corrected shape ran the
fan-out under a context whose deadline was `walkStarted + budget`, checked
before every write and carried into every INSERT. Rule: *post-hoc timing is
not an execution bound.*

**(c) Every blocking operation inside a protected window must consume that
window.** HEAD and ancestry reads were bound to the walk-pin deadline; the
progress LWTs (anchor, cursor, exhaustion, re-anchor) initially were not, and
each is one Paxos round on the session timeout (`database.timeout`,
configurable, default 10 s). The corrected shape bound them to the same
absolute deadline (and deliberately **not** to the 30 s ancestry context, so a
HEAD/parent timeout still cannot erase walked progress). Rule: *if the safety
proof depends on X finishing before deadline D, every blocking operation that
belongs to X is context-bound to D or has another enforced bound strictly
inside D.*

**(d) Deadlines must be absolute and shared.** A fresh `now + 30 s` context
created after a process pause does not know the pause happened. One absolute
anchor (`walkStarted`) for the pre-step deadline and the classifier deadline
is what made the pause cases fail closed.

**(e) Identity separation must be structural.** The short TTL must be
unable to reach the durable identity by construction (the helper derives the
transient referrer from the durable id it is handed; the TTL-taking primitive
is unexported), not by caller discipline.

### 3.3 The property that killed #222

The final defect was **not** "the classifier is unbounded" — that was fixed.
It was:

> #222 inserts a new pre-pass before the stable-owner handoff that `main`
> already performs, and that handoff has **no upper bound**.

The handoff (UNKNOWN → durable 35 d renewal; REACHABLE → `fs:` promotion) is
`main`'s sequential per-block fan-out: N `LOCAL_QUORUM` INSERTs, one driver
attempt each (gocql without a `RetryPolicy`), each bounded only by
`database.timeout`. In this code path:

```text
N               = blocks of one fs_object — no constant bound
database.timeout = configurable, validated > 0 only (default 10 s)
fan-out         ≈ N × database.timeout in the worst case
```

so there is no provable

```text
walk fan-out + classifier + stable-owner handoff  <  walk-pin TTL
```

for all admissible inputs. Cutting the handoff short at a deadline is not an
option either: it would leave blocks with less than `main` writes. What could
be proved was only "every walk pin is valid when the handoff **begins**",
which §3.5 shows is not enough. #222's last commit stated the resulting
guarantee honestly as *conditional* (`h(i) ≤ w(i) + slack`, `slack = TTL −
(S − walkStarted)`, never less than the 5 min margin). A conditional
guarantee that `main` does not need is a regression, however it is worded.

### 3.4 Counterexample 1 — partial walk fan-out (refutes "only a pre-existing residual")

Block B has not yet received `:walk`. Its prior `pub:` expires at t = 12 m.

```text
main
  classifier                      0.5 m
  durable renewal reaches B      +8 m
  stable(B) = 8.5 m  <  12 m               NO GAP

#222
  walk pre-pass runs             10 m, fails before reaching B
  B never receives :walk
  fallback durable renewal starts, reaches B after +8 m
  stable(B) = 18 m
  prior pub(B) expired at        12 m       ZERO-REF 12 m → 18 m
```

`main` safe, #222 unsafe. Both audits of #222 had filed "blocks not yet
reached during the walk fan-out" under `DISCOVERY-SCALE`; this schedule shows
it is a **THIS-PR regression**: `main` would have kept B live and the added
pre-pass is what delays B's stable owner past its prior expiry.

### 3.5 Counterexample 2 — successful pre-pass + long handoff

```text
walk fan-out                20 m   (within #222's budget)
classifier                  0.5 m
handoff prefix to block B   70 m
prior pub(B) expires at     75 m
walk pin(B) written ≈ 20 m, expires at 80 m

main
  stable(B) = 0.5 m + 70 m = 70.5 m  <  75 m           NO GAP

#222
  stable(B) = 20 m + 0.5 m + 70 m = 90.5 m
  prior pub(B) expired 75 m, walk pin(B) expired 80 m    ZERO-REF 80 m → 90.5 m
```

`main` safe, #222 unsafe. "The bridge is alive when the handoff begins" is
not "the bridge is alive until each block has a stable owner".

### 3.6 A side effect worth recording

A fail-closed pre-step bound (fan-out over budget → no classifier → renew and
retry) means a repair whose pre-step *always* exceeds the budget (large N,
slow writes) is **never classified**: it is renewed every visit forever, like
a permanently UNKNOWN row. That is a progress (liveness of the repair) cost
the next design must weigh explicitly, not a safety defect.

---

## 4. Rules derived from #222 (mandatory for any pre-step design)

**4.1 Main-liveness non-regression** (invariant 1 above). For every block and
every admissible schedule: `main` safe ⇒ new design safe. Never accept "the
new mechanism usually provides more liveness"; compare the whole timeline
against `main`.

**4.2 Work inserted before main's handoff consumes the old liveness budget.**
Any `NEW PRE-STEP → main handoff` design must prove one of:

```text
A. the pre-step duration is hard-bounded AND the bridge covers the complete
   delay through the final stable-owner write of every affected block;
B. the pre-step cannot delay the stable-owner write;
C. another continuously valid owner covers the entire added delay.
```

**4.3 Start bound ≠ end-to-end bound.** "Protected at classifier start" and
"protected at handoff start" do not imply "protected through handoff
completion". For a per-block fan-out reason **per block**: old owner
expiry(i), temporary owner interval(i), stable owner write time(i).

**4.4 Partial fan-outs are first-class states.** A sequential fan-out fails
as `blocks 1..K written, block K+1 failed, K+1..N untouched`. Analyze the
three classes separately and compare each against `main`.

**4.5 Crash ≠ error return.** A fallback that runs after an error does not
cover a process/VM death halfway through the pre-step. Specify: crash after 0
writes, after K/N writes, after the complete pre-step, during the classifier,
during the handoff, after the stable owner but before cleanup.

**4.6 Retry backoff is not a liveness guarantee.** The process-local retry
hint (up to 6 h) keeps nothing alive. A durable repair row is not itself
block liveness.

**4.7 TTL is not ownership.** A TTL-bounded reference says only "this row may
remain until T". It does not prove the owner still exists, that the operation
completes before T, or that another owner exists before T. Every
TTL → stable-owner handoff needs a temporal or structural proof.

**4.8 Multi-DC.** Local absence ≠ destructive authority; removing liveness on
absence needs global-enough authority; unavailable authority → fail closed.

---

## 5. Process lessons

**Severity ≠ scope.** Classify every finding as
`Severity: P0/P1/P2` × `Scope: THIS-PR / FOLLOW-UP / PRE-X1 / PRE-GC / GENERAL`:

```text
introduced or worsened by the PR                → THIS-PR blocker
pre-existing but falsifies the PR's guarantee   → blocker, or narrow the claim
pre-existing and does not touch the objective   → follow-up
```

"Discovered while auditing this PR" ≠ "introduced by this PR" — but #222
showed the inverse too: **narrowing the claim does not turn a real regression
into a follow-up.** If `main` safe / new branch unsafe exists, it is THIS-PR
even if the document calls the guarantee "conditional".

**Stop rule.** If a fix that started bounded begins to need any of

```text
new durable witness table · producer generations · lease recycling ·
periodic re-fencing · persistent per-visit ownership · new cleanup scheduler ·
cross-generation compaction · new background recovery protocol
```

stop the PR and return to design review. Those ideas are not banned; they
need their own design proof and must not appear incrementally inside a PR
that began as "move the renewal before the classifier".

**Why this cost two PRs.** Both started from a local transformation
("move/add liveness around the classifier") and discovered progressively that
the real contract is distributed ownership + TTL continuity + arbitrary crash
+ partial fan-out + multi-DC visibility + unlimited retries + bounded durable
state. For this class of problem: **proof first, runtime second.** The next
attempt starts with counterexamples and invariants, not code.

---

## 6. Design gate for the next attempt (mandatory before any runtime)

### 6.1 Phase table

The design must fill this table with no cell resting on "normally", "should
be quick", "retry will fix it", "the TTL is long enough" or "cold path":

| Phase | Liveness owner | Duration bound | Crash result | Retry recovery | Cleanup authority |
| --- | --- | --- | --- | --- | --- |
| hydrate | | | | | |
| any pre-step | | | | | |
| classifier | | | | | |
| progress writes | | | | | |
| handoff (renewal / fs: promotion) | | | | | |
| settlement | | | | | |

### 6.2 Adversarial matrix — on paper first

For each case give: `main` timeline, new timeline, last valid reference, next
valid reference, whether a zero-ref interval exists, whether durable state
grows with the retry count.

```text
 1. prior pub has plenty of TTL
 2. prior pub is about to expire
 3. prior pub expires during the classifier
 4. prior pub expires during any new pre-step
 5. partial pre-step fan-out (1..K written, K+1 failed, rest untouched)
 6. pre-step deadline reached
 7. crash after the first pre-step block
 8. crash after K/N blocks
 9. crash immediately before the classifier
10. pause/resume across any deadline
11. classifier UNKNOWN
12. classifier immediately REACHABLE
13. classifier error
14. progress LWT timeout
15. durable renewal partial failure
16. stable fs: promotion partial failure
17. repair row cleared concurrently
18. repair row requeued concurrently
19. local DC blind to the row
20. another DC unavailable
21. retry delay at its maximum (currently 6 h)
22. persistent UNKNOWN for unlimited visits
23. arbitrarily large N
24. maximum / adversarial database.timeout
25. main-safe / replacement-safe comparison for every timing-sensitive case
```

### 6.3 Special gate — unlimited retries

Answer explicitly: what remains after 1, 10, 1 000, unlimited visits? Prove
durable state stays structurally bounded. Not accepted as a bound: "old
visits usually finish", "GC will eventually clean it", "we cap the retry
count", "we reuse the newest token" — without an ownership/coverage proof.

### 6.4 Special gate — ABA / generations

If the design introduces any reusable identity (token, lease, generation,
epoch, slot, witness), answer before code: *can a decision made for
generation N mutate, fence or delete generation N+1?* If the answer depends
on timing or TTL, the design is not accepted yet.

### 6.5 Special gate — coverage

A coverage proof binds the exact owner/generation, the exact
fs_object/repair identity and the exact block set. A "newer" producer never
replaces another by temporal order alone.

---

## 7. What #220 and #222 did produce

Reusable knowledge, independent of the rejected mechanisms:

```text
- exact distinction between transient and durable liveness
- local absence cannot authorize global cleanup
- EACH_QUORUM fail-closed cleanup rule (certified for one replica per DC)
- partial fan-out handling
- retry-delay vs TTL mismatch
- a deadline must bound actual execution
- progress LWTs must share the liveness window
- durable state introduced by retries must remain structurally bounded
- a TTL-bounded, stable-identity over-retention residual is not by itself a
  rejection reason; reclaiming it "perfectly" is what grew into #220
- coverage must bind the exact generation and block set
- a stale generation must never fence a newer generation
- pre-main work can itself be a liveness regression
- a handoff must be proven end-to-end, not only until its start
```

Evidence that exists only in the closed branches and may be consulted as
prototypes: #220's 71 mutation legs and 16 real 3-DC witness legs; #222's unit
models (deterministic-clock walk crossing the prior expiry, partial-walk
visit, fan-out deadline, progress-LWT deadline), mutation legs M1–M25 and the
real-Cassandra `renewal_before_classify` / directed 3-DC cleanup-authority
legs. None of it is in `main`; none of it is adopted here.

---

## 8. Operational model and questions for the next design

This section records how the **normal** case actually works in `main` and
the questions the next design must answer. Every fact cites `main`
(`d6936323b`). Nothing here is a decision; the hypothesis at the end is
marked as such.

### 8.1 The 35-day `pub:` TTL is a crash/recovery backstop, not publication latency

A file becomes visible when **HEAD publishes the commit**. `fs:` does not
decide visibility; it is the permanent ownership of the blocks for GC. The
content funnels try to complete `pub: → HEAD → fs:` **inside the request**:

```text
materialize blocks (up: provisional refs)
→ create fs_object / tree / commit id
→ stage the attempt pin        pub:<commitID>           35d   (stagePendingPublishedFiles)
→ queue the durable repair row published_block_reference_repairs, one per fs_object
→ insert the commit
→ HEAD CAS                     (UpdateLibraryHeadFromSnapshot)
→ immediately promote          fs:<fsID> per block, then remove pub:<commitID>
                               (promotePendingPublishedFiles → db.PromotePublishAttemptReferences:
                                8 attempts, 50 ms → 400 ms backoff)
→ clear the repair row         (clearPendingPublishedFileRepairs)
→ request completes
```

`internal/api/v2/files.go` `CreateFile` (stage L1494, queue L1503, HEAD
L1522, promote L1536, schedule-on-failure L1538, clear-on-success L1539) is
the template. `HEAD → fs:` is therefore milliseconds to seconds in the
normal case. The durable repair exists for the **abnormal** interval in
which HEAD may already be visible but permanent `fs:` ownership did not
finish (promotion failed after retries, ambiguous HEAD CAS, crash).

### 8.2 Funnel characterization (`main`)

| Funnel | Stage attempt pin | Queue durable row | HEAD | Promote in request | On promote failure | On success |
| --- | --- | --- | --- | --- | --- | --- |
| v2 `CreateFile` (`files.go` L1494–L1539) | `pub:<commitID>` | yes, before HEAD | `UpdateLibraryHeadFromSnapshot` | `promotePendingPublishedFiles` | `schedulePendingPublishedFileRepairs` | clear rows |
| v2 `UploadFile` (`files.go` L3751–L3807) | same | same | same | same | same, label `UploadFile` | clear rows |
| OnlyOffice save (`onlyoffice.go` L1365–L1415) | same | same | same | same | same, label `OnlyOffice` | clear rows |
| batch copy/move destination (`batch_operations.go` L785–L832) | same | same | same | same | same, label `BatchOperationDestination` | clear rows |
| SeafHTTP multi-block upload (`seafhttp.go` L3349–L3387, `finalizeSeafHTTPPublishedBlockReferences` L3199) | via `stageSeafHTTPPublishAttemptReferencesFn` | `queuePublishedFSObjectBlockReferenceRepairFn` | same | `promoteSeafHTTPPublishAttemptReferencesFn` | `schedulePublishedFSObjectBlockReferenceRepairFn` | clear row |
| Sync commit publish (`sync.go` `finalizeSyncCommitBlockDeltaAndSettleRepairIntent` L5080) | Sync attempt identity (`pub:<publishAttemptID>`, R25) | `publishRepairQueueFn` per fs_object | `updateLibraryHeadWithStats` | `finalizeSyncCommitBlockDelta` | `scheduleSyncCommitBlockReferenceRepairs` | `clearSyncCommitBlockReferenceRepairsFn` |

Exceptions and edges:

- HEAD **conflict** → cleanup of the attempt (`CleanupFailedPublishAttempt`)
  and the queued rows are cleared; HEAD **ambiguous/other error** → the
  request returns the error with the attempt pin and the durable row
  intact: the repair worker is the only settler (the R31 ambiguous-HEAD
  case).
- The other `UpdateLibraryHead` callers in `files.go` (create/rename/delete
  directory, rename/delete file, revert, copy within a repo, batch delete)
  do not stage new blocks and do not queue repairs.
- The immediate scheduler (`SchedulePublishedBlockReferenceRepair`,
  `publish_repair.go` L1741) runs **one** repair attempt in a goroutine
  after `RetryBackoff(1)` (`libraryHeadMutationRetryDelay` = 50 ms, capped
  400 ms, jitter 25 ms), deduplicated per key in a process-local
  `sync.Map`. It does **not** consult the 5 min advisory lease. If it fails
  or the process dies, only the durable sweep remains.

### 8.3 Repair worker facts (`main`)

```text
StartPublishedBlockReferenceRepairer          server.go L275, on EVERY node (no leader election)
  startup: runPublishedBlockReferenceRepairSweep
  then every publishedBlockReferenceRepairSweepInterval = 1 min

runPublishedBlockReferenceRepairSweep         publish_repair.go L1646
  one goroutine; for bucket 0..31 (publishedBlockReferenceRepairBuckets):
    SELECT every row of the bucket, then sequentially per row:
      progress-only residue      → reap cells, skip
      process-local retry hint in the future → skip
      created_at newer than 30 s (publishedBlockReferenceRepairStaleAfter) → skip
      lease_expires_at in the future → skip        (advisory only, never authority)
      otherwise: hydrate → classify → settle / renew   (§1)
    on error: next retry hint = now + clamp(age, 5 min, 6 h)   (process-local sync.Map)

new row: lease_expires_at = created_at + publishedBlockReferenceRepairPreCASLease (5 min)
retry:   publishedBlockReferenceRepairRetryBase = 5 min … publishedBlockReferenceRepairRetryMax = 6 h,
         stored only in publishedBlockReferenceRepairNextRetryAt (memory; lost on restart;
         "not an upper bound on discovery visit interval", publish_repair.go L96–L106)
row TTL: none — published_block_reference_repairs WITH default_time_to_live = 0
         (internal/db/migrations/001_initial_schema.cql); a row lives until explicit settlement
```

So, absent the immediate scheduler, a new repair's first durable visit is
about **5 minutes** after it was queued, against a **35-day** pin; a
persistently UNKNOWN row is revisited every 5 min growing to every 6 h, on
every node. Multiple nodes can visit the same row concurrently (the 35-day
race of `ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01` is one consequence).

### 8.4 Expected cadence is not a proven bound

```text
normal settlement   <<   retry cadence   <<   pub TTL
 ms – seconds            5 min – 6 h          35 days

EXPECTED OPERATIONAL BEHAVIOR : seconds / minutes to the first repair visit
PROVEN HARD BOUND             : none
```

The 1-minute ticker is not a 1-minute guarantee: a sweep lists 32 buckets
and processes every row sequentially, each row can spend the classifier
(30 s + progress LWTs) and two per-block fan-outs, so a large backlog or one
slow row delays every row behind it; a server outage, an unavailable
Cassandra, a slow bucket listing or a process restart (which also drops the
retry hints) add unbounded lag. There is no proof today of *"every pending
repair is visited at least once every N minutes"* for arbitrary load. That
gap is the core of `ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01`.

### 8.5 Two `pub:` identities, two owners

| Identity | Written by | TTL | Lifetime |
| --- | --- | --- | --- |
| `pub:<commitID>` (v2) / `pub:<publishAttemptID>` (Sync) — the **attempt pin** | the publishing request, before HEAD (`stagePendingPublishedFiles`) | 35 d | removed by promotion in the request; otherwise expires |
| `pub:<repo:commit:fsID>` — the **repair-owned pin** | the repair worker, after an unresolved classifier (`renewPublishedBlockReferenceRepairLivenessFn`, identity `publishedBlockReferenceRepairLivenessAttemptID`) | 35 d | stable per repair row: every renewal rewrites the same rows and refreshes the TTL; no producer per visit; removed at settlement or by the gone-check compensation, otherwise expires |

Every timeline of the next design must name which owner covers each block
at each instant. Note that `db.AddBlockReference` writes `created_at = now`
on every INSERT (`block_references.go` L1400), so **the last successful
refresh of a given identity is durable per block** (`created_at` /
`TTL(created_at)` of the `block_references` row) — at the cost of one read
per block; the repair row itself records neither (`lease_expires_at` is
scheduling advice; `created_at` is the queue time).

### 8.6 The problem, stated smaller

```text
normal:      HEAD → immediate fs: → done
exceptional: HEAD → promotion did not finish → durable repair remains
             → the temporary pub: must stay continuously alive → eventually fs:
```

The question is therefore **not** "how can the classifier survive for 35
days?" but:

> How can a durable pending repair always retain enough liveness margin
> **before** any operation that consumes that margin?

### 8.7 DESIGN HYPOTHESIS — NOT YET PROVEN: liveness maintenance separate from classification

`main` couples them (`visit → classify → if unresolved, renew`); the
classifier is the emergency mechanism that rescues a pin near expiry, which
is what #220 and #222 tried to patch around. The hypothesis to investigate:

```text
pending repair
→ maintain / refresh the repair-owned pub: with a large margin, independently
→ classify on a repair that already has a safe margin
→ eventually settle to fs:
```

`LIVENESS MAINTENANCE ≠ REACHABILITY CLASSIFICATION`. It is interesting
because the existing numbers (`seconds–minutes` expected response, `5 min –
6 h` retry cadence, `35 d` TTL) suggest the margin already exists and may
only need to be used correctly, without walk pins, per-visit witnesses,
generations, lease recycling or re-fencing. It is not #220 restored:
#220 was pre-classifier renewal **plus** a perfect-cleanup requirement
**plus** a per-visit witness protocol. The hypothesis is only

```text
stable repair identity
+ TTL-bounded over-retention may be acceptable
+ maintain liveness far before expiry
+ do not couple safety to classifier timing
```

If any of *new witness table · per-visit token · generation · epoch · lease
recycling · re-fencing scheduler* reappears while pursuing it: STOP, back to
design review (§5).

### 8.8 Questions the next design must answer before any runtime

**A. How is remaining liveness known?** `lease_expires_at` is not the pin's
expiry and `created_at` is not the last renewal. Options to evaluate, not
choose: derive it from `block_references` (`TTL(created_at)`, per block, see
§8.5); a small durable column on the repair row; or a protocol shape that
never needs to persist it.

**B. What margin, derived not invented.** `SAFE_MARGIN` must come from
`worst discovery lag + worst renewal fan-out + retry/restart allowance +
safety reserve`. Today the first two are unbounded (§8.4, §3.3); write that
down rather than picking a number.

**C. Partial renewal of B1..BN.** `B1..BK` renewed, `B(K+1)` failed,
`B(K+2)..BN` untouched: what TTL remains on the untouched blocks, when is
the next attempt expected (not guaranteed), may `B1..BK` simply stay renewed
(stable identity keeps durable state bounded; no cleanup of a successful
prefix just because the visit failed)?

**D. Crash during renewal** (before the first write, after K/N, after all,
before the classifier) — but answer three separate questions each time:
SAFETY (is any block less protected than in `main`?), STORAGE (did some
blocks merely gain another 35 d?), PROGRESS (when does work continue?). Do
not convert storage over-retention into a safety failure.

**E. Is cleanup of the renewed pin needed at all?** `renew stable
repair-owned pub: → classify → concurrent settlement removes the row →
worker crashes → the renewed pin remains ≤ 35 d`. If that is bounded,
stable-identity, non-accumulating and cannot cause under-retention, the
answer may be "accept it" — the option #220 never evaluated in isolation
(§2.2).

**F. The cross-DC gone-check** (`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01`)
is part of the design, but do not assume #222's `EACH_QUORUM` decider is
the only answer. If a refreshed TTL pin is no longer compensated
aggressively, does the destructive gone-check need to exist? Could "row
disappeared after renewal" simply mean *keep the TTL-bounded pin and let it
expire*? That would remove a destructive cross-DC decision from the protocol
entirely.

**G. Prolonged outage and a GC safety interlock.** If safety depends on the
liveness maintainer running often enough, what happens after days without a
healthy sweep? A conceptual option: `liveness-maintenance health stale →
destructive GC fails closed`. Today GC (`internal/gc/gc.go`) gates a cycle
on `Enabled`, the leader lease and the grace period; it has **no**
watermark for "last successful complete repair-liveness sweep", "oldest
pending repair" or "oldest liveness refresh", and `internal/metrics` exposes
nothing about the repair worker. Whether GC may destroy blocks while the
subsystem that keeps temporary references alive has not proven health for
days is an explicit design question, not an implementation item here.

**H. Observability needed before choosing.** None of these exist today:
oldest pending repair age, pending repair count, time since the last
successful complete sweep, maximum sweep duration, repairs processed per
sweep, oldest successful repair-owned `pub:` refresh, renewal failures,
promotion failures after HEAD. Recorded as future observability; not added
by this PR.

**I. Quantify the reality first.** With existing code/tests: normal
`HEAD → fs:` (8 promotion attempts, 50–400 ms); promotion failure → first
async repair (~50 ms, one attempt, process-local); durable sweep (startup,
1 min, 5 min initial lease, 32 buckets, sequential); persistent UNKNOWN
(5 min → 6 h, memory only); pub TTL 35 d; repair-row TTL none. Then compare
the expected timeline with the guaranteed one — they differ today.

### 8.9 Model to freeze

```text
The 35-day pub TTL is not publication latency.

Normal publication attempts to complete  pub → HEAD → fs  inside the request.

A durable repair exists for the abnormal interval in which HEAD may already
be visible but permanent fs: ownership has not finished.

The next design should therefore investigate whether liveness maintenance can
be treated as a separate responsibility from reachability classification:
keep every pending repair comfortably alive first, then classify/settle it.

This is a design hypothesis, not an adopted solution.

Before accepting it we must establish:
- a real bound or fail-closed policy for discovery/maintenance lag;
- partial-fanout and crash behavior;
- how renewal freshness is known;
- multi-DC cleanup authority, or whether destructive compensation can be
  removed entirely;
- behavior during prolonged worker/server outage;
- whether destructive GC must be interlocked with liveness-maintenance health.
```

### 8.10 Shape of the next PR

Still design/characterization, not runtime. One question:

> Can repair liveness be maintained independently of classification, using
> the existing stable repair-owned `pub:` identity and 35-day TTL, while
> accepting bounded over-retention and failing closed when maintenance
> health cannot be guaranteed?

It must produce: the exact current-state timeline; the scheduling/TTL facts
of §8.3; hard guarantees vs operational expectations; a candidate protocol,
preferably without new durable entities; the partial-fan-out proof; the
crash proof; the multi-DC decision; the prolonged-outage / GC-interlock
decision; the adversarial matrix of §6.2; and the explicit
`main-safe ⇒ new-safe` proof. Only after that audits clean does a runtime
PR start.

---

## 9. Decision

```text
PR #220 and PR #222 are rejected approaches, not failed implementations
to be repaired incrementally.

#220 showed that moving durable liveness ahead of the classifier leaves,
on a crash after a concurrent clear, a TTL-bounded ownerless pin; that
trying to reclaim that pin perfectly grew into a per-visit durable witness
protocol; and that this protocol was not shown to combine unlimited retries
with structurally bounded durable state. It did not show that explicitly
accepting and bounding that over-retention is invalid.

#222 demonstrated that replacing that durable state with a transient
pre-pass avoids the storage-growth problem but can delay main's existing
stable-owner handoff. Because that handoff has no unconditional duration
bound, schedules exist in which main maintains continuous liveness while
the added pre-pass creates a zero-reference interval.

Therefore ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01 remains OPEN
(P1, PRE-X1 / PRE-GC).

The next attempt must begin as a design proof, starting from the
operational model and the open questions of §8 (liveness maintenance
separate from classification is a hypothesis there, not a decision). It
must demonstrate
main-liveness non-regression, crash safety, partial-fanout safety,
unlimited retries, and structurally bounded durable state before any
new production runtime is implemented. If it accepts a TTL-bounded
over-retention residual, it must say so and bound it explicitly.

ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01 (main's local absence
decision in the renewal gone-check) is recorded as a separate open issue
and is not fixed here.

No runtime mechanism from #220 or #222 is adopted by this documentation PR.
```
