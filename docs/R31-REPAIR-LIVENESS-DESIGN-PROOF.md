# R31 publish-repair liveness — design proof (independent liveness maintenance)

**Status:** CLOSED — **OUTCOME B: HYPOTHESIS REJECTED** (candidate V0 falsified by D7/D3; see D12). Documentation / characterization only.
**Issue:** `ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01` — OPEN, P1, PRE-X1 / PRE-GC.
**Parent:** `7bac9c125` (`main` containing #223).
**Branch:** `docs/r31-repair-liveness-design-proof`.
**Prerequisite (source of record):** [PUBLISH-REPAIR-LIVENESS-REJECTED-DESIGNS.md](./PUBLISH-REPAIR-LIVENESS-REJECTED-DESIGNS.md) — the two invariants, the design gate (§6), the operational model of `main` (§8) and the open questions (§8.8). This file does not restate them; it answers them.
**Not this PR:** Go runtime, CQL/schema, tables, worker or GC changes, the `EACH_QUORUM` gone-check fix (`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01`), metrics, a new scheduler, witnesses, generations, leases, re-fencing, cherry-picks from #220/#222.

## The one question

> Can repair liveness be maintained independently of reachability
> classification, using the existing stable repair-owned `pub:` identity
> and 35-day TTL, while accepting bounded over-retention and failing closed
> when maintenance health cannot be guaranteed?

## The two admissible outcomes

```text
A. PROVEN DESIGN
   a small candidate protocol; main-safe ⇒ new-safe for every block and
   every admissible schedule; structurally bounded durable state under
   unlimited retries; partial fan-out safe; crash safe; outage / fail-closed
   policy decided; phase table and 25-case matrix complete
   → the next PR may be a minimal runtime implementation

B. HYPOTHESIS REJECTED
   a concrete counterexample (main safe, candidate unsafe; or unbounded
   state; or a needed mechanism that trips the stop rule), recorded with
   the same rigor as §3.4 / §3.5 of the rejected-designs record
   → no runtime; back to design review
```

Ending with "it probably works" is not an outcome. Ending with B is a
success of this PR.

## Stop rule (inherited, restated)

If making the candidate work requires any of *new witness table · per-visit
producer/token · generation / epoch · lease recycling · periodic re-fencing ·
new cleanup scheduler · cross-generation compaction · new background
recovery protocol*: STOP, record why, and return outcome B. Do not grow the
candidate.

---

## Method

Every **material proof claim** in this document is classified as one of
three kinds, and says which; explanatory prose and cross-references are
not tagged:

```text
FACT       verified in main, cited (file:line or test name), or carried by
           the canonical characterization (PUBLISH-REPAIR-LIVENESS-REJECTED-DESIGNS.md §8)
           which holds the citation
DERIVED    follows from cited facts by an argument written out here
OPEN       not established; what would settle it is stated
```

Every liveness argument is made **per block**, on a timeline with, for
block *i*: `old owner expiry(i)`, `new temporary owner interval(i)`,
`stable owner write time(i)`, and the comparison against `main` at every
instant (§4.3 of the record). Every failure case is split into **SAFETY /
STORAGE / PROGRESS** (§4.5). Expected cadence is never used as a bound
(§8.4).

---

## Deliverables

### D1 — Current-state timeline, per block (`main`)

Owner and TTL of every block at every phase, for one fs_object with blocks
`B1..BN`, in the normal case and in the abnormal case:

```text
up:<op>                 provisional, 48 h          materialization
pub:<attempt>           35 d                       staged before HEAD
HEAD                    —                          visibility
fs:<fsID>               permanent                  request-local promotion, sequential per block
repair row              no TTL                     queued before HEAD, cleared on success / by owner
pub:<repo:commit:fsID>  35 d, stable per row       written by an unresolved repair visit (sweep or immediate scheduler)
```

Acceptance: the timeline names, for each of `B1..BN`, which identity
covers it at each instant of `main`'s abnormal path (promotion failed after
HEAD), including the interval the classifier can consume, and marks the
zero-ref interval of the issue. Reuse §8.1–§8.5; add nothing that is not
cited.

### D2 — Candidate protocol V0 (smallest variant first)

The first and only variant to study before any other:

```text
pending repair visit
→ ensure the repair-owned pub:<repo:commit:fsID> has safe liveness
     (refresh in place; same identity; 35 d)
→ accept any orphaned refresh as bounded ≤ 35 d over-retention
     (no perfect compensation; no destructive gone-check on the refresh path)
→ classify
→ REACHABLE → promote fs: → settle
   UNKNOWN   → retain (liveness already refreshed)
```

using only the existing repair row, the existing stable identity and the
existing TTL. The deliverable states the protocol precisely (which visit
step writes what, under which precondition, with which consistency) and
lists what it deliberately does **not** do (compensate, witness, fence).
Whether it is correct is decided by D3–D9, not asserted here.

### D3 — Non-regression proof: `main` safe ⇒ V0 safe

Contract: **for every schedule where `main` is safe, V0 must be safe** —
for every block, once a visit occurs. (Guaranteeing that a visit occurs
before all prior liveness expires is the separate PRE-GC problem
`ISSUE-PUBLISH-REPAIR-DISCOVERY-SCALE-01`; this proof does not absorb it.)

V0 inserts refresh work before its own classifier and handoff — in the
REACHABLE case the refresh work completed before classification (the full
fan-out only when the refresh completes N/N; on stop-at-first-error, the
prefix consumed up to the failure) — and therefore **can** delay a block's
next valid owner relative to `main` (whether it does in a given schedule
depends on both executions' later phases); for UNKNOWN it moves `main`'s
own renewal fan-out in front of the classifier. Per block *i* the
quantities are instants of two different executions:

```text
w(i)              instant V0 attempts the refresh of i   (an attempt, not an owner)
r(i)              instant a successful refresh of i is acknowledged / the 35 d owner exists
V0_next_owner(i)  instant V0 installs its next valid owner for i:
                    successful refresh  → V0_next_owner(i) = r(i)
                    failed / unreached  → V0_next_owner(i) > w(i)  (a later phase or the next visit)
m(i)              instant main's visit installs its next valid owner for i
```

The proof obligation for a non-regressive V0, made precise by D7, is the
per-block condition

```text
V0_next_owner(i) ≤ m(i)          for every i, under every admissible schedule
```

which for successfully refreshed blocks reads `r(i) ≤ m(i)`.

and it must be either shown structural or refuted with a schedule in
which V0's next valid owner for a block arrives later than `main`'s next
valid owner for that block would have (the two fan-outs are the same
*kind* of per-block reference write over the same logical blocks, not the
same primitive, list or order — see D7). The
two schedules that killed #222 (§3.4, §3.5) are replayed explicitly.
Acceptance: a written argument per timing-sensitive case, or a
counterexample (→ outcome B).

### D4 — SAFE_MARGIN, derived or declared unprovable

```text
SAFE_MARGIN > discovery lag + renewal fan-out + restart/retry allowance + safety reserve
```

Each term is FACT, DERIVED or OPEN. Today discovery lag (§8.4) and renewal
fan-out (§3.3, `N × database.timeout`) have no hard bound. If any term is
unbounded the deliverable states **NO FINITE SAFE_MARGIN CAN BE PROVEN**
and hands over to D5; it does not pick a number.

Scope note: the *discovery lag* term belongs to problem B of the record
(a repair not visited until after all prior liveness is gone —
`DISCOVERY-SCALE` / `ZERO-REF`), not to problem A that this PR studies (a
visit occurs while liveness exists and the classifier consumes it). D4/D5
record what a margin would need; they are **not** a precondition for D3's
verdict, and the GC-health interlock of D5 is a PRE-GC adjacent
requirement, not a requirement for closing this issue.

### D5 — Prolonged-outage model and fail-closed policy

Decide whether a future interlock

```text
repair-liveness maintenance health not proven fresh → destructive GC must not proceed
```

is necessary to turn a scheduler without a hard bound into a safety
guarantee, what "health" would have to mean (last complete successful
sweep? oldest pending repair? oldest refresh?), and how it composes with
GC's existing gates (`Enabled`, leader lease, grace,
`ValidateDestructiveGCTopology`). Decision only; no implementation.
Acceptance: a stated policy that, together with D4, makes the safety claim
of D3 not depend on expected cadence.

### D6 — How remaining liveness is known

Compare, with the ambiguous-write and cross-DC semantics of §8.5 in mind:

```text
A. block_references TTL(created_at)   observation; needs certified semantics
B. a small durable freshness field    on the repair row; needs its own write semantics
C. a protocol that never needs exact freshness   e.g. V0 refreshes unconditionally per visit
```

Acceptance: one option chosen for V0 with its consistency requirement
written out, or the finding that V0 needs none (C) and why.

### D7 — Partial fan-out, per group

`B1..BK` refreshed, `B(K+1)` failed, `B(K+2)..BN` untouched. For each
group: old owner expiry, new owner interval, crash behavior, next recovery,
zero-ref possible? — compared with `main`. Includes the question whether
`B1..BK` may simply stay refreshed (stable identity ⇒ no cleanup of a
successful prefix).

### D8 — Crash cases, three answers each

```text
before the refresh · after 1 block · after K/N · after N/N ·
before the classifier · during the classifier · after a concurrent repair-row clear
```

For each: SAFETY (any block less protected than in `main`?), STORAGE (extra
≤ 35 d?), PROGRESS (when does work continue, and by whom — durable sweep,
immediate scheduler, Sync idempotent retry?).

### D9 — Unlimited retries: bounded durable state

State after 1, 10, 1 000, ∞ visits, under partial writes, crashes and
concurrent visits from several nodes (§8.3: the sweep runs on every node).
The hypothesis to prove: the same `pub:<repo:commit:fsID>` refreshed in
place is `O(blocks)`, not `O(visits)`. Acceptance: a proof, or a schedule
that accumulates.

### D10 — Gone-check and the cross-DC decision

Ask first: if a refreshed pin may expire naturally, why delete it when the
repair row disappears? If V0 would make that destructive decision
unnecessary, record exactly that — *V0 would make it unnecessary* — while
`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01` **remains OPEN in
`main` until a runtime PR actually removes or replaces that path**. If V0
still needs a destructive absence decision, record its required authority
and keep the issue as a separate PR. Also state what V0 does with
`ISSUE-PUBLISH-REPAIR-OWNED-PUB-CLEANUP-RACE-01` (unchanged, widened, or
narrowed).

### D11 — Phase table and the 25-case adversarial matrix

The phase table of §6.1 (owner / duration bound / crash result / retry
recovery / cleanup authority for hydrate, liveness refresh, classifier,
progress writes, handoff, settlement) with no cell resting on "normally",
"should be quick", "retry will fix it", "TTL is long enough" or "cold
path"; then the 25 cases of §6.2, each with `main` timeline, V0 timeline,
last valid reference, next valid reference, zero-ref interval?, state grows
with retries? The OPEN list lives here.

### D12 — Decision

Outcome A or B, in the form of §9 of the record, plus — only for A — the
exact scope of the minimal runtime PR (#225): which functions change, which
tests/mutations/integration legs are required, what stays out.

---

## Order of work (each step is one commit; each commit is auditable alone)

```text
1. D1  current-state timeline                          (facts only)
2. D2  V0 stated precisely                              (no claims)
3. D7 + D8  partial fan-out and crash, per block       (where #222 died: do it before the proof)
4. D3  non-regression proof, incl. §3.4 / §3.5 replayed against V0
5. D9  unlimited retries
6. D4 → D5  margin, then outage / fail-closed policy
7. D6  freshness knowledge
8. D10 gone-check / cross-DC
9. D11 phase table + matrix (closing every OPEN or moving it to D12)
10. D12 decision; CURRENT_WORK / OPEN-WORK-INDEX / CHANGELOG one-liners + links
```

Steps 3–5 are where a counterexample is most likely; if one appears, stop
the sequence, write it up, and go to D12 with outcome B. *(This is what
happened: step 3 produced the counterexample; step 4 formalized outcome B
in D3; steps 5 onward were not performed.)*

## Merge criteria (conditional on the outcome)

```text
Common
1.  no runtime / schema / config / migration / script files changed
2.  every material proof claim classified FACT / DERIVED / OPEN; every material
    FACT used by the rejection proof cited to main or to the canonical
    characterization that carries the citation
3.  D12 is A or B
4.  the stop rule was not tripped, or tripping it is the recorded reason for B
5.  ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01 stays OPEN; X1 / W2-R31 / GC unchanged
6.  git diff --check clean

Outcome A additionally
7.  D3 replays §3.4 and §3.5 explicitly and proves main-safe ⇒ candidate-safe
8.  D4 derives SAFE_MARGIN from bounded terms or states it cannot be proven
9.  D5 states an outage policy; nothing implemented
10. D7 / D8 cover every group and every crash point with SAFETY / STORAGE / PROGRESS
11. D9 states the durable-state bound under ∞ visits
12. D10 answers whether the destructive gone-check is needed; the issue is not fixed here
13. D11 complete: phase table without soft cells; 25 cases with the six outputs each
14. D12 names the exact scope of the runtime PR

Outcome B additionally
7.  the candidate is stated precisely (D2)
8.  the counterexample(s) are demonstrated: main-safe ⇒ candidate-unsafe (D3, D7)
9.  the relevant crash / partial-fan-out evidence is recorded (D7, D8)
10. D12 = REJECTED, stating exactly what the rejection covers and does not cover
11. the remaining deliverables are marked NOT REQUIRED because the candidate
    failed an earlier mandatory gate; none is silently skipped
```

---

## Sections (filled in the order above)

### D1 — Current-state timeline

The facts this proof needed are those of the record §8.1–§8.5 (identities,
TTLs, visit order, the two sequential handoff fan-outs, the retry/sweep
cadence), restated where used in the *Setting* of D3 and the *Setup* of
D7 with citations. The full per-block timeline was not expanded beyond
that: the candidate failed at D7/D3 (outcome B), and D1's remaining detail
would only serve deliverables that are NOT REQUIRED.

### D2 — Candidate protocol V0

Stated precisely; nothing here is claimed correct. Names refer to `main`.

```text
visit(repair row R with staged_block_ids B1..BN, in that order)

1. hydrate R                                   (as main; row gone → no-op)
2. refresh:  for i = 1..N in order:
               INSERT pub:<repo:commit:fsID> for Bi   USING TTL 35 d, LOCAL_QUORUM
               (db.AddPublishAttemptReferences with the durable repair identity;
                same rows main's UNKNOWN renewal writes; stop at the first error,
                exactly as that primitive does today)
             refreshErr := first error, or nil
             NO post-write gone-check, NO compensation of what was written
3. classify  (as main: bounded classifier, progress LWTs, unchanged)
4. settle:
     REACHABLE → promote fs: per block (as main, PromotePublishAttemptReferences
                 retry policy) → remove the durable repair pub: → delete R (as main)
     UNKNOWN / classifier error → retain R; preserve main's settlement and
                 error semantics unchanged (settlePublishedBlockReferenceRepair
                 still returns "unknown; retain queued repair" for UNKNOWN with
                 no classifier error, which is what drives main's retry hint);
                 return errors.Join(refreshErr, settleErr / classifyErr);
                 NO second renewal (already done in 2)
     row disappearance observed by main's existing checks (hydrate, the
                 classifier's Gone reading via its progress CAS, StillPending
                 before the renewal and in the reflex renewal after a failed
                 settlement) → follow main's gone / no-op behavior for that
                 check; settlePublishedBlockReferenceRepair itself does not
                 re-read the repair row;
                 the pins already written in step 2 are not compensated and
                 remain until their TTL (≤ 35 d over-retention, accepted).
                 A concurrent delete not observed by those checks does not by
                 itself stop a REACHABLE settlement already in progress from
                 installing fs: — exactly main's existing semantics
```

What V0 deliberately does not do: no transient identity; no compensation
of a refresh that raced a clear; no destructive gone-check on the refresh
path; no witness, token, generation or lease; no change to the classifier,
to `fs:` promotion, to settlement, or to discovery. A refresh error does
**not** skip the classifier (lesson §3.2(a) of the record): blocks `1..K`
received the refreshed owner; blocks `K+1..N` received no owner from V0's
refresh and remain dependent on their prior owners until V0's later
handoff or the next visit (their state is described, not compared: V0 has
already consumed wall-clock time, so it is not necessarily the
counterfactual `main`'s state — see D7); REACHABLE settlement may still
install `fs:` for them.

Consistency and freshness inputs are left to D6; V0 as written refreshes
unconditionally on every visit (option C of D6) and therefore needs no
freshness knowledge.

### D3 — Non-regression proof → formal rejection of V0

**Claim (DERIVED from FACTs in D7).** V0 violates *main-liveness
non-regression*: there exist admissible schedules in which `main` keeps a
block continuously live and V0 opens a zero-reference interval.

**Setting.** One repair row for one fs_object; a concrete block `B` of
that fs_object. `main`'s visit: `hydrate → classifier → handoff`, the
handoff being a sequential per-block reference write that stops at its
first error — on UNKNOWN a `pub:` write per entry of `staged_block_ids`
(FACT: `addPublishAttemptReferencesRows`, `block_references.go`
L676–L688), on REACHABLE an `fs:` write per block of the fs_object's
resolved block list (FACT: `RegisterFSObjectBlockReferences` →
`resolveStoredBlockIDs`, `fs_helpers.go` L1596–L1618). These are the same
*kind* of write over the same logical blocks under the normal invariant,
not the same primitive, list or order; the proof does not depend on any
such equivalence. V0's visit: `hydrate → refresh (a pub: write per entry of
staged_block_ids, stop at first error) → classifier → handoff`. Notation
for block `B`:

```text
w(B)              instant V0 attempts the pre-refresh of B
m(B)              instant main would install its next valid owner for B
                  (35 d pub: on UNKNOWN, fs: on REACHABLE)
V0_next_owner(B)  instant V0 installs its next valid owner for B
old(B)            expiry of B's prior owner
```

Admissible schedules include transient write failures and time-varying
write latency (nothing in `main` excludes either).

**The argument, once, for a concrete block `B`.** Suppose V0's refresh
leaves `B` without a new owner — either because the refresh **never
attempted** `B` (the fan-out stopped at an earlier error), or because the
attempt at `w(B)` failed with an outcome **known not to have applied** the
mutation. (An ordinary Cassandra INSERT can fail *ambiguously* and still
have applied — record §8.5 — so `refreshErr != nil` alone is never taken
to mean "no owner"; the witness schedule chooses a known-unapplied
failure, e.g. a coordinator refusal before the write, which nothing
excludes.) `main` may later successfully install an owner at `m(B)` (a
write that failed at `w(B)` is not bound to fail at `m(B)`). V0 inserted pre-step work before
classification and handoff; under time-varying latency V0's later phases
*could* run faster than the counterfactual `main`'s and compensate, so
`V0_next_owner(B) > m(B)` is **not** claimed for every schedule. It is
enough that **there exist admissible schedules** in which the pre-step
consumed positive time, `B` obtained no owner, and the phases after the
pre-step take no less time in V0 than in `main` — the simplest being equal
per-operation latencies from the classifier onward, which nothing excludes
— so that `V0_next_owner(B) > m(B)`. Choose one such schedule and an
`old(B)` with

```text
m(B) ≤ old(B) < V0_next_owner(B)
```

Then `main` keeps `B` continuously live and V0 has a zero-reference
interval `[old(B), V0_next_owner(B))`. `m(B)` and `V0_next_owner(B)` are
instants of two different executions and are never assumed to share the
same `C` or `h(B)`. Two counterexample **classes** (how `B` was left
without a new owner) × two classifier outcomes give four block-level
witnesses; one partial-failure execution can expose both classes at once.
**Class S is the minimal rejection witness**: it does not depend on the
semantics of the failed write at all. **Class F is conditional** on a
known-unapplied failure and is supporting evidence.

**Class F — `B` is the block whose refresh failed with a known-unapplied
outcome** (conditional witness; for an ambiguous error no absence of
`B`'s owner is inferred from `refreshErr`).
- *UNKNOWN:* `m(B)` = the instant `main`'s UNKNOWN renewal writes `B`
  (which may succeed). V0 classifies UNKNOWN and, by D2, performs no second
  renewal, so `V0_next_owner(B)` = the next visit, which the process-local
  hints target at 5 min – 6 h and nothing bounds (§8.3–§8.4 of the record).
- *REACHABLE:* `m(B)` = the instant `main`'s promotion writes `fs:` for
  `B`. V0's promotion writes `fs:` for `B` only after the pre-step time it
  consumed; no owner written by V0 covers `B` in between.

**Class S — `B` is in the untouched suffix** (the refresh stopped at an
earlier error — ambiguous or not, it stopped — and never attempted `B`,
so `B` definitely has no owner from V0's refresh; minimal witness).
- *UNKNOWN:* `main`'s later renewal is not bound to stop where V0's refresh
  stopped; it may succeed at the failed block and continue, installing
  `m(B)`. V0 performs no second renewal: `V0_next_owner(B)` = the next
  visit.
- *REACHABLE:* as class F / REACHABLE — `main` installs `fs:` at `m(B)`;
  V0 only after the pre-step time, with no owner in between.

**Why no in-PR variant survives.** All four witnesses share one cause:
a sequential pre-step placed before `main`'s handoff consumes wall-clock
time for every block it does not manage to cover, and nothing in V0
guarantees that time is recovered before `main`'s next valid owner for
that block, `m(B)`, would have been installed.
Covering *reached* blocks with a 35-day owner (the difference from #222)
protects exactly those blocks and no other. (b′) shrinks the uncovered set
to the failed blocks but lengthens the pre-step for them; (b″) is `main`.
Any repair would have to guarantee that a block the pre-step fails to cover
is written no later than `main` would have written it — which is
equivalent to not having the pre-step for that block.

**Replay of #222's schedules (§3.4, §3.5 of the record).** §3.4 (partial
pre-pass, block never reached) is counterexample 3 with a 35-day pin
replacing the 1-hour walk pin: the pin's length is irrelevant to a block
that never received it. §3.5 (successful pre-pass, long handoff) does *not*
apply to V0 in its original form — a reached block holds a 35-day owner
through any handoff length — which is why V0 was worth one experiment; it
is defeated by failure schedules instead, not by handoff length.

**Verdict.** `main`-safe ⇒ V0-safe is **false**. Outcome B.

### D4 — SAFE_MARGIN

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.

### D5 — Prolonged outage and fail-closed policy

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.

### D6 — Remaining-liveness knowledge

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.

### D7 — Partial fan-out (falsification attempt: the not-yet-refreshed block)

**Setup (FACT).** `main`'s UNKNOWN renewal (`addPublishAttemptReferencesRows`,
`block_references.go` L676–L688: one `pub:` INSERT per entry of
`staged_block_ids`, list order, stop at first error) and its REACHABLE
promotion (`RegisterFSObjectBlockReferences` → `resolveStoredBlockIDs`,
`fs_helpers.go` L1596–L1618: one `fs:` INSERT per block of the fs_object's
resolved list, stop at first error) are the same *kind* of sequential
per-block reference write over the same logical blocks under the normal
invariant — not the same primitive, list or guaranteed order. V0's
refresh (D2 step 2) is `main`'s UNKNOWN-renewal primitive with the durable
repair identity, executed before the classifier. Nothing below relies on
the two fan-outs being identical.

Notation, per block *i*, as in D3: `old(i)` = expiry of the prior owner
(attempt pin or an earlier repair-owned pin); `m(i)` = the instant `main`'s
visit installs its next valid owner for *i* (after its classifier, along
its handoff); `w(i)` = the instant V0's visit attempts the refresh of *i*;
`V0_next_owner(i)` = the instant V0's visit (or, failing that, the next
visit) installs its next valid owner for *i*. `main` and V0 are different
executions; no duration is shared between them, and every comparison
below picks a witness schedule in which the phases after V0's pre-step do
not compensate the pre-step time (equal per-operation latencies from the
classifier onward suffice, and nothing excludes them).

**Group 1 — `B1..BK`, refreshed.** V0's 35-day owner for block *i*
exists from `r(i)`, the acknowledged refresh (not from the attempt
`w(i)`); `main` gives it a new reference at `m(i)` (35-day pin on UNKNOWN,
`fs:` on REACHABLE). Zero-ref in V0 requires `old(i) < r(i)`; zero-ref in
`main` requires `old(i) < m(i)`. So V0 is safe wherever `main` is safe
**iff `r(i) ≤ m(i)`** (DERIVED). SAFETY: conditional on
that inequality. STORAGE: on REACHABLE the block carries an extra 35-day pin
until settlement removes it; on a concurrent clear, ≤ 35 d over-retention.
PROGRESS: unchanged.

**Group 2 — `B(K+1)`, the failed write.** *Corrected on review: this
group is a regression on its own, conditionally.* The refresh of `B(K+1)`
fails at `w(K+1)`. An ordinary INSERT can fail ambiguously and still have
applied (record §8.5), so an error alone does not prove `B(K+1)` has no
owner; this group is a witness only for a **known-unapplied** failure
(admissible: e.g. a coordinator refusal before the write). For such a
failure, nothing in `main` says that a write which failed at `t1` fails
again at `t2` (a transient error, an unavailable replica that recovers) —
`main`'s corresponding write happens at a different instant, `m(K+1)`, and
may succeed. Group 3 below needs no such condition. With `B = B(K+1)`, in a witness schedule where
V0's post-pre-step phases do not compensate the pre-step time:

```text
UNKNOWN    main: renewal(B) succeeds at m(B)  → 35 d owner
           V0:   refresh(B) failed; D2 performs NO second renewal
                 → V0_next_owner(B) = next visit (targeted 5 min – 6 h, unbounded)
           choose old(B) in [m(B), V0_next_owner(B))  ⇒  main safe, V0 zero-ref

REACHABLE  main: fs(B) at m(B)
           V0:   fs(B) at V0_next_owner(B) > m(B), no new owner in between
           choose old(B) in [m(B), V0_next_owner(B))  ⇒  main safe, V0 zero-ref
```

SAFETY: **worse than `main`** (DERIVED). STORAGE: none. PROGRESS: next
visit. This is not repaired by a "second renewal on UNKNOWN" either: in
the same witness schedule that write still lands after `m(B)`.

**Group 3 — `B(K+2)..BN`, untouched.** V0 wrote nothing for them. On
UNKNOWN, `main`'s later renewal is **not** bound to stop where V0's refresh
stopped (a failure at `w(K+1)` says nothing about `main`'s write at
`m(K+1)`): it may succeed at `K+1` and continue through the suffix,
installing `m(i)` for every `i > K+1`, while V0 — no second renewal —
leaves them on `old(i)` until the next visit. That is one more
`main`-safe / V0-unsafe schedule, of the same shape as group 2. On
REACHABLE, `main` installs `fs:` at `m(i)`; V0 installs it later by the
pre-step time consumed before the classifier, with no new owner in
between. **This is the exposure the audit predicted**, and it is real for
group 3 on both paths: nothing covers blocks the refresh never reached. Its size is the refresh prefix up to the failure, not a full
fan-out, and it also exists in `main` whenever a visit's classifier exceeds
what `old(i)` had left — but a delay that `main` does not have is a
regression by the non-regression rule (§4.1), regardless of its size.

**The audited schedule (no failure, late block B).** With no write
failure, B is group 1: V0's owner for B exists from `r(B)`, `main`'s from
`m(B)`. The schedule "old(B) expires after `main` covers B but before V0
covers B" requires `r(B) > m(B)`: V0's acknowledged refresh of B later than `main`'s
classifier **plus** handoff would — plausible-sounding to exclude because
V0 starts writing at once and `main` first spends `C`, but the two
fan-outs are not the same list or order, and even if they were, only a
stationary latency model would exclude it (V0 starts writing at once
while `main` first classifies);
under a time-varying model (writes slow during V0's early refresh, fast
later when `main` would have written) it can happen. *Corrected on
review:* stationarity is **not a safety invariant of the system** — no
runtime contract in `main` bounds `latency(write at t1)` by
`latency(write at t2)`, and a slow Cassandra interval followed by a fast
one is an admissible schedule. Therefore `r(B) ≤ m(B)` is **not
structurally guaranteed** (DERIVED), and group 1 is conditional at best.
This schedule is no longer needed to reject V0 (group 2 does it
directly); it is recorded so that no future variant relies on it.

**Consequence.** V0 as stated in D2 fails main-liveness non-regression
on multiple admissible `main`-safe / V0-unsafe witnesses (DERIVED): two
classes — the failed block (group 2) and the untouched suffix (group 3) —
each under UNKNOWN and under REACHABLE, and one partial-failure execution
can expose both. No other owner covers the exposed blocks
(the attempt pin is the same `old(i)`). The variants that were visible
without a new mechanism do not repair it and are recorded only so they
are not re-proposed: **(b′) continue-on-error refresh** removes group 3 but
not group 2 — a block whose refresh failed with a known-unapplied outcome
(admissible) still has no new owner while V0 now spends the *rest* of the
fan-out before classifying, so its `main` write is delayed even more; **(b″) promote first on REACHABLE,
refresh only on UNKNOWN** is `main`'s order and reopens the original
issue. By the charter's own rule (a counterexample stops the sequence),
the experiment ends here: D3 records the formal rejection, D12 is
outcome B. No variant is evaluated inside this PR.

### D8 — Crash cases

Process/VM death (no error return, no fallback). "Next" = the next visit
by the durable sweep (targeted 5 min – 6 h, no bound) or the immediate
scheduler; on Sync direct-HEAD also the client's idempotent retry.

The comparison is against the counterfactual `main` visit under the same
admissible schedule as D3 — time-varying latency, transient failures — so
"at the same instant" `main` may already have finished its classifier and
reached blocks V0's refresh had not. Two regimes (DERIVED throughout):

| Crash point | SAFETY vs `main` | STORAGE | PROGRESS |
| --- | --- | --- | --- |
| before the refresh (after hydrate) | identical to `main` crashing after hydrate: nothing written | none | next visit |
| after 1 block, or after K/N, **refresh so far successful** | prefix `B1..BK`: V0 holds a fresh 35-day repair-owned pin from `r(i)`; the comparison with `main` is schedule-dependent (`r(i) ≤ m(i)` is not structural, and under the same adversarial schedule the counterfactual `main` may already hold permanent `fs:`). Unreached suffix `B(K+1)..BN`: **not proven no-worse than `main`** — V0 spent the pre-step before crashing, and `main` may already have installed `m(i)`; D7 group 3 / D3 class S apply | +35 d on the prefix if the row is concurrently cleared (accepted) | next visit refreshes in place (same identity); its own refresh prefix delays the suffix again |
| after K/N, **refresh already failed on some block(s)** | prefix: fresh 35-day pin from `r(i)`, comparison schedule-dependent. Failed blocks: D7 group 2 / D3 class F apply when the failure is known-unapplied (an ambiguous error may have installed the owner). Unreached suffix: as the row above (class S, unconditional) | as above | next visit |
| after N/N (full successful refresh), before the classifier | every block holds a 35-day owner from its `r(i)` on; whether `main` would already have installed `m(i)` earlier is the non-structural `r(i) ≤ m(i)` comparison of D7, so "no block less protected" is not claimed | ≤ 35 d on all if cleared concurrently | next visit classifies; the refresh repeats (in place) |
| during the classifier, **after a full successful refresh** | as `main` crashing during its classifier, plus the 35-day owners from step 2 (same caveat as the row above) | same | progress LWTs are durable (as `main`) |
| during the classifier, **after a partial / failed refresh** (D2 continues to the classifier on `refreshErr`) | prefix: fresh 35-day pin from `r(i)`, comparison schedule-dependent; unreached blocks: no owner from V0's refresh (class S); failed blocks: no owner only when the failure is known-unapplied (class F) — and V0 has spent the pre-step plus part of its own classifier before `V0_next_owner(i)`; D7 groups 2–3 / D3 apply | prefix only | progress LWTs durable; the suffix still waits |
| after a concurrent repair-row clear (row gone, then crash) | the refreshed pins are ownerless: no under-retention (the clearer left `fs:` or removed a proven non-live publication, §8.2 / GONE-CHECK entry) | ≤ 35 d, stable identity, no accumulation | none needed |

Summary: a **full successful** refresh before the crash gives every block
a 35-day cover from its acknowledged refresh `r(i)`; a **partial or
failed** refresh before the crash gives the prefix a fresh pin and leaves
the failed / unreached blocks with exactly the non-regression exposure D7
and D3 demonstrate. Neither "strictly more liveness than `main`" nor "more
protected than `main`" is claimed for any row, because both would require
the non-structural `r(i) ≤ m(i)`.

### D9 — Unlimited retries

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.

### D10 — Gone-check and cross-DC

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.
`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01` remains OPEN in `main`,
unchanged by this PR.

### D11 — Phase table and adversarial matrix

**NOT REQUIRED** — the candidate failed the mandatory safety gate (D3 /
D7) before this deliverable; studying it for a protocol already shown
unsafe would add nothing. Its question stays open for a future design
attempt exactly as stated in the record §8.8.

### D12 — Decision

```text
OUTCOME B — HYPOTHESIS REJECTED (for the candidate as posed)

Candidate V0 — refresh the stable repair-owned pub:<repo:commit:fsID> in
place, as a sequential per-block fan-out placed before the reachability
classifier, accepting any orphaned refresh as ≤ 35 d over-retention — is
rejected. D7 and D3 show multiple admissible main-safe / V0-unsafe
witnesses in two classes — the untouched suffix after a partial refresh
(minimal witness, independent of ambiguous-write semantics), and the
block whose refresh failed with a known-unapplied outcome (conditional,
supporting) — each under UNKNOWN and under REACHABLE (a single
partial-failure execution can expose both), in which main keeps a block
continuously live and V0 opens a zero-reference interval, because the pre-step consumes
wall-clock time for every block it fails to cover and can delay that
block's next owner past the instant main would have installed it. A continue-on-error refresh and a promote-first order do not
repair it and are recorded only so they are not re-proposed.

No runtime is written. ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01
remains OPEN (P1, PRE-X1 / PRE-GC). ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-
AUTHORITY-01 remains OPEN. X1, W2/R31 for this residual and GC activation
are unchanged.
```

**What this rejection covers, exactly (DERIVED).** It covers the
pre-step family *as characterized here*: a sequential, failable, in-visit
fan-out placed ahead of `main`'s handoff **where**

```text
- the pre-step adds wall-clock time before main's stable-owner write;
- a block that fails or is not reached obtains no independent new owner;
- no proven temporal invariant (a hard bound on the added delay together
  with a certified remaining-liveness margin of the prior owner) covers
  that delay.
```

V0 and #222's walk pin satisfy all three conditions. It does **not**
prove that every sequential or failable pre-step is impossible: a design
that provides an independent covering owner for failed/unreached blocks,
or a structural temporal invariant V0 lacks, is outside this proof and
would have to be judged on its own. It does not, by itself, refute the
record's §8.7 hypothesis in its literal form — *liveness maintenance as a
responsibility separate from the classification visit*, i.e. a maintainer
that never delays the visit's own handoff — because such a maintainer adds
no time in front of `main`'s writes. That form was **not** studied here and
carries its own unanswered obligations (bounded state under unlimited
visits, discovery, cross-DC authority, outage policy: §8.8 A–I of the
record). It is left as a question for a future design attempt, not as a
proposal of this PR.

**Lessons to carry (DERIVED; added to the record's list):**

```text
- a sequential pre-step that can fail per block, placed ahead of main's
  stable-owner handoff, is unsafe unless every block it fails to cover has
  an independent new owner or a proven temporal invariant covers the added
  delay: otherwise the pre-step can delay that block's next owner past the
  instant main would have installed it, and nothing recovers that time;
- covering the reached blocks with a long-TTL owner protects exactly those
  blocks; the pin's length is irrelevant to blocks the pre-step never
  reached or failed on;
- stationary latency is not a system invariant; "same kind of write,
  earlier start" does not make r(B) ≤ m(B) structural;
- a transient failure at t1 says nothing about the same write at t2;
  "main would have failed too" is never an argument.
```

**Follow-up decision (2026-09-18): the residual is parked, not pursued
PR after PR.** Two observations make that the right call:

- problem A (a visit arrives while liveness exists and the classifier
  consumes what is left) is practically reachable only inside problem B's
  regime: with a ≤ 30 s classifier and a 35 d TTL, the visit must arrive
  with under a minute of TTL left, i.e. after ~35 days without one
  successful renewal — no visit at all (`DISCOVERY-SCALE`) or every
  renewal failing for 35 days;
- a **fail-closed GC health gate** (record §8.8 G) changes the nature of
  the proof: a gate needs only a *conservative observation* — if it cannot
  show that every pending repair keeps a safe margin, or does not know how
  many there are, or the sweep has not completed recently, destructive GC
  does not proceed; anything unknown counts as unsafe. That tolerates
  exactly the weaknesses that sink the protocol route (§8.5's ambiguous
  writes, cross-DC observation), covers A and B at once without touching
  the visit, and costs progress rather than safety (a permanently UNKNOWN
  repair blocks GC until resolved — acceptable PRE-GC).

What the parked residual blocks: declaring X1 closed; activating
destructive GC on the strength of this liveness guarantee. What it does
not block: ordinary development, the other X1 pieces, characterization,
cleanup, performance, other protocols. Order of the next work, none of it
a renewal variant: (1) repair-worker observability (§8.8 H — oldest pending
repair, pending count, time since the last complete successful sweep,
sweep duration, repairs per sweep, renewal / post-HEAD promotion
failures), the prerequisite of any gate; (2)
`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01` as its own scoped PR;
(3) `ISSUE-PUBLISH-REPAIR-PROGRESS-PAXOS-DOMAIN-01` and cross-chunk
cycles; (4) the remaining X1 pieces; then (5) the strategic choice —
an indefinite liveness guarantee versus 35 d + monitoring + a fail-closed
GC gate — taken against real metrics. The two directions D12 leaves open
(repair-owned coverage established before HEAD; a maintainer that never
delays the handoff) stay available if the strong guarantee is ever needed.

### Subsequent scoped follow-up

On 2026-09-19, a separate PRE-X1 / PRE-GC follow-up closed
`ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01`: the post-renewal local
gone observation no longer authorizes deletion of the repair-owned `pub:`;
the existing 35-day TTL is accepted as the bounded over-retention fallback.
This dated note does not change the historical outcome of PR #224 or close
`ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01`, W2/R31, X1, or GC.
