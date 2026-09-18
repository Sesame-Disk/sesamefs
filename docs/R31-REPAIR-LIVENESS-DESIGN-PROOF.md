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
pub:<repo:commit:fsID>  35 d, stable per row       written only by an unresolved worker visit
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

For every block and every admissible schedule. The argument must cover
the two schedules that killed #222 (§3.4, §3.5) explicitly and show why V0
does not delay `main`'s stable-owner write for any block: in V0 the
pre-classifier write **is** a 35-day owner for that block, not a bridge to
one, so the per-block comparison is `refresh(i) ≤ main's next reference(i)`
plus a 35-day cover — or the proof fails and the reason is recorded.
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
repair row disappears? If V0 removes the destructive gone-check
conceptually, record that `ISSUE-PUBLISH-REPAIR-GONE-CHECK-XDC-AUTHORITY-01`
is superseded by V0's design (still not fixed here). If V0 still needs a
destructive absence decision, record its required authority and keep the
issue as a separate PR. Also state what V0 does with
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

_pending_

### D3 — Non-regression proof

_pending_

### D4 — SAFE_MARGIN

_pending_

### D5 — Prolonged outage and fail-closed policy

_pending_

### D6 — Remaining-liveness knowledge

_pending_

### D7 — Partial fan-out

_pending_

### D8 — Crash cases

_pending_

### D9 — Unlimited retries

_pending_

### D10 — Gone-check and cross-DC

_pending_

### D11 — Phase table and adversarial matrix

_pending_

### D12 — Decision

_pending_
