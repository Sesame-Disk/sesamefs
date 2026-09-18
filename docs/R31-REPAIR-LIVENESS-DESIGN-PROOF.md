# R31 publish-repair liveness — design proof (independent liveness maintenance)

**Status:** IN PROGRESS — design proof. Documentation / characterization only.
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

Every claim in this document is one of three kinds, and says which:

```text
FACT       verified in main, cited (file:line or test name)
DERIVED    follows from cited facts by an argument written out here
OPEN       not established; listed in §D11 with what would settle it
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
the sequence, write it up, and go to D12 with outcome B.

## Merge criteria

```text
1.  no runtime / schema / config / migration / script files changed
2.  every claim tagged FACT / DERIVED / OPEN; every FACT cited
3.  D3 replays §3.4 and §3.5 explicitly
4.  D4 either derives SAFE_MARGIN from bounded terms or states it cannot be proven
5.  D5 states an outage policy; nothing implemented
6.  D7 / D8 cover every group and every crash point with SAFETY / STORAGE / PROGRESS
7.  D9 states the durable-state bound under ∞ visits or the accumulating schedule
8.  D10 answers whether the destructive gone-check is needed; the issue is not fixed here
9.  D11 complete: phase table without soft cells; 25 cases with the six outputs each
10. D12 is A or B; A names the exact scope of the runtime PR; B names the counterexample
11. the stop rule was not tripped, or tripping it is the recorded reason for B
12. ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01 stays OPEN; X1 / W2-R31 / GC unchanged
13. git diff --check clean
```

---

## Sections (filled in the order above)

### D1 — Current-state timeline

_pending_

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
     row gone during 3/4 → nothing written, nothing removed; the refreshed
                 pins expire (≤ 35 d over-retention, accepted)
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

### D3 — Non-regression proof

_pending_

### D4 — SAFE_MARGIN

_pending_

### D5 — Prolonged outage and fail-closed policy

_pending_

### D6 — Remaining-liveness knowledge

_pending_

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

**Group 2 — `B(K+1)`, the failed write.** Same INSERT as `main`'s UNKNOWN
renewal would issue for that block at `C + h(K+1)`; if the environment
rejects it at `w(K+1)`, `main`'s later attempt may or may not succeed —
V0 gains nothing and loses nothing for this block relative to `main`'s
UNKNOWN path (both leave it on `old(K+1)` until the next visit). On
REACHABLE, V0 still classifies (D2) and promotion retries up to 8 times,
as in `main`. SAFETY: not worse than `main`. STORAGE: none. PROGRESS: next
visit (sweep / scheduler), as in `main`.

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
h(i)`); under an adversarial non-stationary model (writes slow during V0's
early refresh, fast later when `main` would have written) it can. Whether
the admissible-schedule definition of the design gate allows the latter is
the crux; it is recorded as OPEN for D3, not decided here.

**Consequence.** V0 as stated in D2 is **not** unconditionally
non-regressive: group 3 on REACHABLE after a partial refresh is a
`main`-safe / V0-unsafe schedule (DERIVED), and group 1 depends on the
OPEN latency condition. D3 must therefore either (a) refute the group-3
schedule by showing another owner covers those blocks in V0 (none is
visible in D2: the attempt pin is the same `old(i)`); (b) change V0 so a
refresh failure does not delay `fs:` for unreached blocks. Two shapes of
(b) are visible without any new mechanism, and both are candidates to
evaluate, not adopted: **(b′) continue-on-error refresh** — the same
per-block INSERT loop, but it does not stop at the first error: every
reachable block gets its 35-day owner at `w(i)`, the failed blocks alone
stay on `old(i)` exactly as in `main`'s UNKNOWN path, and group 3 ceases to
exist (group 2 becomes "each failed block, not worse than `main`"); its
cost is a fan-out whose duration no longer shortens on failure, which feeds
the OPEN latency condition, not safety; **(b″) promote first on
REACHABLE and refresh only on UNKNOWN** — which is *exactly `main`'s
order* and reopens the original issue, so it is not a fix; or (c) conclude
outcome B. Any change must be re-run through this section before D3.

### D8 — Crash cases

Process/VM death (no error return, no fallback). "Next" = the next visit
by the durable sweep (targeted 5 min – 6 h, no bound) or the immediate
scheduler; on Sync direct-HEAD also the client's idempotent retry.

| Crash point | SAFETY vs `main` | STORAGE | PROGRESS |
| --- | --- | --- | --- |
| before the refresh (after hydrate) | identical to `main` crashing after hydrate: nothing written | none | next visit |
| after 1 block | `B1` has a 35-day owner `main` would not have yet; `B2..BN` as in `main` before its handoff; no block less protected | +35 d on `B1` if the row is concurrently cleared (accepted) | next visit refreshes in place (same identity) |
| after K/N | `B1..BK` covered; `B(K+1)..BN` as in `main` — but the *next* visit is where group 3's REACHABLE delay (D7) reappears, now with `w = full prefix of a new refresh` | ≤ 35 d on `B1..BK` | next visit |
| after N/N, before the classifier | every block has a 35-day owner; strictly more liveness than `main` at the same instant | ≤ 35 d on all if cleared concurrently | next visit classifies; the refresh repeats (in place) |
| during the classifier | as `main` crashing during its classifier, plus the 35-day owners from step 2: strictly more liveness | same | progress LWTs are durable (as `main`) |
| after a concurrent repair-row clear (row gone, then crash) | the refreshed pins are ownerless: no under-retention (the clearer left `fs:` or removed a proven non-live publication, §8.2 / GONE-CHECK entry) | ≤ 35 d, stable identity, no accumulation | none needed |

Crash after a *partial* refresh is where V0 is not simply "more liveness":
it is `main`'s state for the unreached blocks with the group-3 delay
attached to the following REACHABLE visit (D7). Everything else is
`main`'s state plus 35-day owners.

### D9 — Unlimited retries

_pending_

### D10 — Gone-check and cross-DC

_pending_

### D11 — Phase table and adversarial matrix

_pending_

### D12 — Decision

_pending_
