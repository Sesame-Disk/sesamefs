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

V0 **does** delay `main`'s permanent-owner write (`fs:`) by one full
refresh fan-out in the REACHABLE case, and for UNKNOWN it moves `main`'s
own renewal fan-out in front of the classifier. What it changes per block
*i* is therefore the instant of the *first new reference*: `w(i)` (refresh
prefix) in V0 versus `C + h(i)` (classifier + `main`'s handoff prefix) in
`main`. The proof obligation, made precise by D7, is the per-block
condition

```text
w(i) ≤ C + h(i)          for every i, under every admissible schedule
```

and it must be either shown structural (same write, same order, refresh
precedes the classifier) or refuted with a schedule in which V0's refresh
reaches a block later than `main`'s classifier plus handoff would have. The
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
`DISCOVERY-SCALE` / `ZERO-REF`), not to problem A that this PR closes (a
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
happened: step 3 produced the counterexample; steps 4 onward were not
performed; D3 holds the formal rejection.)*

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
     UNKNOWN / classifier error → retain R; return refreshErr joined with the
                 classifier error, if any; NO second renewal (already done in 2)
     row gone during 3/4 → no further writes, no compensation; the pins
                 already written in step 2 remain until their TTL
                 (≤ 35 d over-retention, accepted)
```

What V0 deliberately does not do: no transient identity; no compensation
of a refresh that raced a clear; no destructive gone-check on the refresh
path; no witness, token, generation or lease; no change to the classifier,
to `fs:` promotion, to settlement, or to discovery. A refresh error does
**not** skip the classifier (lesson §3.2(a) of the record): blocks `1..K`
are covered, blocks `K+1..N` are exactly as in `main`, and REACHABLE
promotion can still install `fs:` for all of them.

Consistency and freshness inputs are left to D6; V0 as written refreshes
unconditionally on every visit (option C of D6) and therefore needs no
freshness knowledge.

### D3 — Non-regression proof → formal rejection of V0

**Claim (DERIVED from FACTs in D7).** V0 violates *main-liveness
non-regression*: there exist admissible schedules in which `main` keeps a
block continuously live and V0 opens a zero-reference interval.

**Setting.** One repair row with `staged_block_ids = B1..BN`. `main`'s
visit: `hydrate → classifier (C) → handoff`, the handoff being a sequential
per-block fan-out that reaches block *i* at `C + h(i)` and stops at its
first error (FACT: `addPublishAttemptReferencesRows`
`block_references.go` L676–L688; `RegisterFSObjectBlockReferences`
`fs_helpers.go` L1612–L1616). V0's visit: `hydrate → refresh fan-out (same
primitive, same list, same order, reaching block *i* at w(i)) → classifier
→ handoff`. Admissible schedules include transient write failures and
time-varying write latency (nothing in `main` excludes either).

**Counterexample 1 — failed refresh, UNKNOWN.** The refresh of block `B`
fails at `w(B)` (transient). V0 classifies UNKNOWN and, by D2, performs no
second renewal; `B` keeps only `old(B)` until the next visit, which the
process-local hints target at 5 min – 6 h and nothing bounds (§8.3–§8.4 of
the record). `main` classifies UNKNOWN and its renewal writes `B` at
`C + h(B)`, succeeding (a later write is not bound to fail). For any
`old(B)` with `C + h(B) ≤ old(B) < next visit`: `main` continuous, V0
zero-ref.

**Counterexample 2 — failed refresh, REACHABLE.** Same failure; V0
classifies REACHABLE; promotion reaches `B` at `w(K+1) + C + h(B)` where
`w(K+1)` is the refresh prefix consumed before the failure. `main` reaches
`B` at `C + h(B)`. For `C + h(B) ≤ old(B) < w(K+1) + C + h(B)`: `main`
continuous, V0 zero-ref. No owner written by V0 covers `B` in that
interval.

**Counterexample 3 — partial refresh, untouched suffix, REACHABLE.** The
refresh fails at `K+1`; blocks `K+2..N` were never reached. V0's promotion
reaches them at `w(K+1) + C + h(i)`; `main`'s at `C + h(i)`. Same
interval, same conclusion, for every block of the suffix.

**Why no in-PR variant survives.** The three schedules share one cause:
a sequential pre-step placed before `main`'s handoff consumes wall-clock
time for every block it does not manage to cover, and that time is never
recoverable — `main`'s write for that block would already have happened.
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
`block_references.go` L676–L688) and its REACHABLE promotion
(`RegisterFSObjectBlockReferences`, `fs_helpers.go` L1596–L1618) are both
sequential per-block INSERTs into `block_references` over `staged_block_ids`
in list order, and both stop at the first error. V0's refresh (D2 step 2)
is the first of those primitives with the durable repair identity — the
same rows `main` writes on UNKNOWN — executed before the classifier.

Notation, per block *i*: `old(i)` = expiry of the prior owner (attempt pin
or an earlier repair-owned pin); `C` = classifier duration in this visit;
`h(i)` = prefix duration of `main`'s post-classifier fan-out up to block
*i*; `w(i)` = prefix duration of V0's refresh up to block *i*.

**Group 1 — `B1..BK`, refreshed.** V0 gives block *i* a 35-day owner at
`w(i)`; `main` gives it a new reference at `C + h(i)` (35-day pin on
UNKNOWN, `fs:` on REACHABLE). Zero-ref in V0 requires `old(i) < w(i)`;
zero-ref in `main` requires `old(i) < C + h(i)`. So V0 is safe wherever
`main` is safe **iff `w(i) ≤ C + h(i)`** (DERIVED). SAFETY: conditional on
that inequality. STORAGE: on REACHABLE the block carries an extra 35-day pin
until settlement removes it; on a concurrent clear, ≤ 35 d over-retention.
PROGRESS: unchanged.

**Group 2 — `B(K+1)`, the failed write.** *Corrected on review: this
group is a regression on its own.* The refresh of `B(K+1)` fails at
`w(K+1)`; nothing in `main` says that a write which failed at `t1` fails
again at `t2` (a transient error, an unavailable replica that recovers, a
timeout) — `main`'s corresponding write happens at a different time,
`C + h(K+1)`, and may succeed. Then:

```text
UNKNOWN    main: renewal(B) succeeds at C + h(B)  → 35 d owner
           V0:   refresh(B) failed; D2 performs NO second renewal
                 → B stays on old(B) until the next visit (targeted 5 min – 6 h, unbounded)
           old(B) expiring in [C + h(B), next visit)  ⇒  main safe, V0 zero-ref

REACHABLE  main: fs(B) at C + h(B)
           V0:   fs(B) at w(K+1) + C + h(B), no new owner in between
           old(B) expiring in [C + h(B), w(K+1) + C + h(B))  ⇒  main safe, V0 zero-ref
```

SAFETY: **worse than `main`** (DERIVED). STORAGE: none. PROGRESS: next
visit. This is not repaired by a "second renewal on UNKNOWN" either: that
write would land at `w(·) + C + h(B)`, still later than `main`'s.

**Group 3 — `B(K+2)..BN`, untouched.** V0 wrote nothing for them; `main`
would have written nothing for them either on UNKNOWN (its renewal stops at
the same first error) and, on REACHABLE, `fs:` at `C + h(i)` — which V0
also reaches, delayed by the refresh prefix `w(K+1)`: V0's `fs:(i)` lands
at `w(K+1) + C + h(i)`. Zero-ref in V0 requires `old(i) < w(K+1) + C + h(i)`
while `main` was safe with `old(i) ≥ C + h(i)`. **This is the exposure the
audit predicted**, and it is real for group 3 on the REACHABLE path: the
delay `w(K+1)` is not covered by any new owner for blocks the refresh never
reached. Its size is the refresh prefix up to the failure, not a full
fan-out, and it also exists in `main` whenever a visit's classifier exceeds
what `old(i)` had left — but a delay that `main` does not have is a
regression by the non-regression rule (§4.1), regardless of its size.

**The auditor's schedule (no failure, late block B).** With no write
failure, B (position *i*) is group 1: V0 reaches it at `w(i)`, `main` at
`C + h(i)`. The schedule "old(B) expires after `main` reaches B but before
V0 reaches B" requires `w(i) > C + h(i)`: V0's refresh prefix slower than
`main`'s classifier **plus** handoff prefix to the same block, with the
same write on the same list in the same order and no classifier in front.
Under a stationary latency model this cannot happen (`w(i) ≈ h(i) ≤ C +
h(i)`); under a time-varying model (writes slow during V0's early
refresh, fast later when `main` would have written) it can. *Corrected on
review:* stationarity is **not a safety invariant of the system** — no
runtime contract in `main` bounds `latency(write at t1)` by
`latency(write at t2)`, and a slow Cassandra interval followed by a fast
one is an admissible schedule. Therefore `w(i) ≤ C + h(i)` is **not
structurally guaranteed** (DERIVED), and group 1 is conditional at best.
This schedule is no longer needed to reject V0 (group 2 does it
directly); it is recorded so that no future variant relies on it.

**Consequence.** V0 as stated in D2 fails main-liveness non-regression
on three independent schedules (DERIVED): group 2 on UNKNOWN, group 2 on
REACHABLE, group 3 on REACHABLE. No other owner covers the exposed blocks
(the attempt pin is the same `old(i)`). The variants that were visible
without a new mechanism do not repair it and are recorded only so they
are not re-proposed: **(b′) continue-on-error refresh** removes group 3 but
not group 2 — every block whose refresh failed still has no new owner
while V0 now spends the *rest* of the fan-out before classifying, so its
`main` write is delayed even more; **(b″) promote first on REACHABLE,
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
| after 1 block, or after K/N, **refresh so far successful** | prefix `B1..BK`: more protected (35-day owner `main` would not have yet). Unreached suffix `B(K+1)..BN`: **not proven no-worse than `main`** — V0 consumed `w(K)` before crashing, and under a time-varying schedule `main` may already have written those blocks; D7 group 3 / D3 counterexample 3 apply to the delay | +35 d on the prefix if the row is concurrently cleared (accepted) | next visit refreshes in place (same identity); its own refresh prefix delays the suffix again |
| after K/N, **refresh already failed on some block(s)** | prefix: more protected. Failed blocks: D7 group 2 / D3 counterexamples 1–2 apply. Unreached suffix: as the row above | as above | next visit |
| after N/N (full successful refresh), before the classifier | every block is covered by a 35-day owner from its refresh instant `w(i)` on; whether `main` would already have reached block *i* earlier is the non-structural `w(i) ≤ C + h(i)` comparison of D7, so "no block less protected" is not claimed | ≤ 35 d on all if cleared concurrently | next visit classifies; the refresh repeats (in place) |
| during the classifier, **after a full successful refresh** | as `main` crashing during its classifier, plus the 35-day owners from step 2 (same caveat as the row above) | same | progress LWTs are durable (as `main`) |
| during the classifier, **after a partial / failed refresh** (D2 continues to the classifier on `refreshErr`) | prefix: more protected; failed / unreached blocks: no new owner, and V0 has consumed `w(·)` plus part of `C` — D7 groups 2–3 / D3 counterexamples apply | prefix only | progress LWTs durable; the suffix still waits |
| after a concurrent repair-row clear (row gone, then crash) | the refreshed pins are ownerless: no under-retention (the clearer left `fs:` or removed a proven non-live publication, §8.2 / GONE-CHECK entry) | ≤ 35 d, stable identity, no accumulation | none needed |

Summary: a **full successful** refresh before the crash gives every block
a 35-day cover from its refresh instant; a **partial or failed** refresh
before the crash gives the prefix extra liveness and leaves the failed /
unreached blocks with exactly the non-regression exposure D7 and D3
demonstrate. "Strictly more liveness than `main`" is not claimed for any
row, because it would require the non-structural `w(i) ≤ C + h(i)`.

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
rejected. D7 and D3 show three admissible schedules (transient refresh
failure on UNKNOWN; the same on REACHABLE; a partial refresh with an
untouched suffix on REACHABLE) in which main keeps a block continuously
live and V0 opens a zero-reference interval, because the pre-step consumes
wall-clock time for every block it fails to cover and that time is not
recoverable. A continue-on-error refresh and a promote-first order do not
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
  delay: otherwise that block is written later than main would have written
  it, and the delay is unrecoverable;
- covering the reached blocks with a long-TTL owner protects exactly those
  blocks; the pin's length is irrelevant to blocks the pre-step never
  reached or failed on;
- stationary latency is not a system invariant; "same write, same order,
  earlier start" does not make w(i) ≤ C + h(i) structural;
- a transient failure at t1 says nothing about the same write at t2;
  "main would have failed too" is never an argument.
```
