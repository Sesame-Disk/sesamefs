# Publish-Repair Observability

This runbook covers the Prometheus metrics of the published-block-reference
repair worker: the durable repair that runs after a publication is already
visible (HEAD published) but its request-local `fs:` promotion did not
finish. Why this worker exists, how the normal case works and why its
expected cadence is not a proven bound are recorded in
[PUBLISH-REPAIR-LIVENESS-REJECTED-DESIGNS.md](./PUBLISH-REPAIR-LIVENESS-REJECTED-DESIGNS.md)
§8; these metrics are the observability that record asked for (§8.8 H) and
the prerequisite of the fail-closed GC health gate it leaves as a design
question (§8.8 G,
[R31-REPAIR-LIVENESS-DESIGN-PROOF.md](./R31-REPAIR-LIVENESS-DESIGN-PROOF.md)
D12). **They report; they gate nothing.** No protocol, schema or scheduling
behavior changes with them.

## Scrape scope

- SesameFS exposes `/metrics` only on the internal listener path; scrape
  from the node itself, from a private network, or through
  `docker compose exec sesamefs ...` as described in [DEPLOY.md](./DEPLOY.md).
- **Every node runs its own sweep** (`StartPublishedBlockReferenceRepairer`
  is started by every process; there is no leader). Every series below is
  per node and process-local: counters restart at 0 with the process, the
  gauges are re-seeded by that node's next sweep.
- **A node's backlog gauges are its observation, not a global or atomic
  witness.** A sweep is a sequential pass over 32 buckets at the node's own
  consistency: rows can appear and disappear while it runs, and another
  node — in another datacenter especially — may list rows this node does
  not see, or fewer. Nodes and DCs may disagree. `max()` across nodes is a
  **dashboard aggregation only**: `max(publish_repair_pending_rows) = 0`
  does not prove there is no pending repair anywhere. A future
  destructive-GC gate must obtain global-enough authority of its own
  (record §2.6 / §4.8) or fail closed on unknown or disagreement; it must
  not read these gauges as absence.

## What the sweep is

Startup sweep, then one sweep every minute. A sweep lists the 32 buckets of
`published_block_reference_repairs` sequentially and, per row, either reaps
progress-only residue, skips the row (process-local retry hint in the
future; row younger than 30 s; advisory lease in the future) or runs a
repair visit: hydrate → bounded reachability classifier → REACHABLE promote
`fs:` and settle / otherwise renew the durable 35-day pin and retain.
Requests that fail their post-HEAD promotion also schedule one immediate
background visit (~50 ms later, one attempt, process-local).

## Metrics

### `publish_repair_pending_rows`

Gauge. Pending repair rows (rows the sweep could act on; progress-only
residue excluded) observed by the last **complete** sweep on this node —
one that listed every bucket. A sweep with a bucket-listing error leaves it
unchanged, so it never under-reports because part of the backlog was
unreadable.

A pending row is not block liveness: the row has no TTL, the pins it names
do (35 d). This is the repair backlog, not the remaining TTL of any block.

### `publish_repair_oldest_pending_age_seconds`

Gauge. Age of the oldest pending row at the instant the last complete
sweep **finished** (completion time − `created_at`, clamped at 0 for a row
queued while the sweep ran); 0 when there is none. Queue-time age of the
row, not remaining pin TTL. A row that stays pending for days is either persistently UNKNOWN
(renewed every visit, never resolved) or not being renewed at all — the
`visits_total` and `renewal_failures_total` series tell which.

### `publish_repair_last_sweep_started_timestamp_seconds`

Gauge. Unix timestamp of the last sweep start on this node, complete or
not; 0 means never.

### `publish_repair_last_complete_sweep_timestamp_seconds`

Gauge. Unix timestamp at which the last sweep on this node that listed
every bucket **finished** (completion, not start: a 40-minute sweep that
has just completed reads as fresh); 0 means never. Per-row repair failures
do not withhold it (the backlog was observed); a bucket that could not be
listed does.

This is the heartbeat a health gate would read. With a 1-minute cadence:

```text
expr: time() - publish_repair_last_complete_sweep_timestamp_seconds > 600
for: 5m
```

reads as "this node has not fully observed the repair backlog for ten
minutes". It fires on a stuck or very slow sweep and on a Cassandra that
cannot list a bucket; it does not fire on rows that fail to settle.

### `publish_repair_sweep_duration_seconds_{bucket,sum,count}`

Histogram of sweep wall-clock durations, buckets from 100 ms to 1 h. The
sweep is sequential and every visited row may run the bounded classifier
(30 s) plus per-block fan-outs, so a sweep longer than its own 1-minute
cadence is normal under backlog — and is exactly the evidence that the
cadence is not a bound. Watch p95 against the cadence, not against an
absolute number.

### `publish_repair_sweep_rows_total{outcome=...}`

Counter of what the sweep did with each listed row:

- `visited`: ran a repair visit (see `publish_repair_visits_total` for how
  it ended);
- `skipped_retry_hint`: process-local retry hint still in the future
  (5 min → 6 h after a failed visit; lost on restart);
- `skipped_young`: row younger than the 30 s staleness cutoff;
- `skipped_lease`: advisory `lease_expires_at` still in the future (5 min
  after queueing; never authority, scheduling only);
- `residue_reaped` / `residue_reap_not_applied` / `residue_reap_failed`:
  progress-only residue rows (no staged blocks; never pending). `reaped`
  only when the conditional reap applied; `not_applied` when the CAS lost
  to a concurrent requeue or reaper and nothing was removed by this sweep;
  `failed` on error.

`skipped_*` rows are still pending and still counted in
`publish_repair_pending_rows`.

### `publish_repair_visits_total{outcome=...}`

Counter of repair visits by how they ended, from the sweep and from the
immediate scheduler alike:

- `ok`: no error — the row was settled (REACHABLE, `fs:` installed, pin
  removed, row deleted) or found gone and the visit was a no-op;
- `retained`: the ordinary unresolved outcome — the classifier could not
  prove reachability, the durable pin was renewed, the row is kept for
  retry. A steady rate of `retained` for the same backlog means rows that
  never resolve;
- `failed`: any other error — a renewal or settlement that did not
  complete, a hydrate error. **This is the counter to watch**: a `retained`
  visit kept the row alive as `main` relies on; a `failed` one may not have.

### `publish_repair_renewal_failures_total`

Counter of durable 35-day pin renewals that returned an error. The renewal
is a sequential per-block fan-out that stops at its first error, so one
increment may be a partial renewal (some blocks refreshed, the rest not),
and the failing write itself may have applied ambiguously (record §8.5).
A rising rate therefore means **renewal success cannot be shown** —
liveness maintenance is unhealthy or uncertain — not that any block has
lost all its owners: the prior pins may well be alive. Every increment is
also a `failed` visit.

### `publish_repair_post_head_reconciliation_failures_total{funnel=...}`

Counter of **publications** — once each, whatever the number of files or
fs_objects they carried — that published HEAD and did not complete their
request-local post-HEAD reconciliation (permanent `fs:` promotion and
attempt-pin cleanup), and were handed to the repair path: the rate at
which the abnormal post-HEAD interval is entered. It is incremented at the
funnel sites (`schedulePendingPublishedFileRepairs`,
`finalizeSeafHTTPPublishedBlockReferences`,
`scheduleSyncCommitBlockReferenceRepairs`), not in the scheduler, so
Sync's one-call-per-fs_object scheduling does not multiply it. The label
is the funnel's own label: `CreateFile`, `UploadFile`, `OnlyOffice`,
`BatchOperationDestination`, the SeafHTTP operation names
(`commitUploadedFileMultiBlock`, …) and the Sync operation names. Note
that Sync's finalize can fail after `fs:` was installed (e.g. on attempt-pin
cleanup), so this is "reconciliation did not complete", not strictly
"`fs:` missing".

In a healthy deployment this counter is close to flat; `HEAD → fs:` is
attempted inside the request with up to 8 promotion attempts.

### `publish_repair_immediate_repairs_total{outcome=ok|failed|deduplicated}`

Counter of the scheduling of the one-shot background visit those requests
run: `deduplicated` when a repair for the same key was already scheduled
(the call is dropped), otherwise the run's outcome. This is scheduling
volume — Sync schedules one key per fs_object of a commit — not
publications. A `failed` immediate repair leaves the row to the durable
sweep (first durable visit ≈ 5 min after queueing).

## Suggested dashboard panels

### Backlog and age (dashboard aggregation; per-node observations, see scope)

```promql
max(publish_repair_pending_rows)
max(publish_repair_oldest_pending_age_seconds)
```

### Sweep health per node

```promql
time() - publish_repair_last_complete_sweep_timestamp_seconds
histogram_quantile(0.95, sum by (le) (rate(publish_repair_sweep_duration_seconds_bucket[15m])))
```

### Why rows are pending

```promql
sum by (outcome) (rate(publish_repair_visits_total[15m]))
rate(publish_repair_renewal_failures_total[15m])
```

### How rows enter the repair path

```promql
sum by (funnel) (rate(publish_repair_post_head_reconciliation_failures_total[1h]))
sum by (outcome) (rate(publish_repair_immediate_repairs_total[1h]))
```

## What is deliberately not here

- **Remaining TTL of the repair-owned pins** ("oldest successful
  repair-owned refresh"). Cassandra exposes it per block as
  `TTL(created_at)` of the `block_references` row, but reading it costs one
  query per block per pending row per sweep, and as an *observation* it is
  not a certified last-successful-refresh witness (ambiguous writes,
  cross-DC; record §8.5). Whether a gate reads it on demand, treats the
  unknown as unsafe, or needs no exact freshness at all is the gate
  design's decision (design proof D6), not a metric to add blindly.
- **Any gate.** Destructive GC keeps its existing gates (`Enabled`, leader
  lease, grace, topology); it does not read these series.
