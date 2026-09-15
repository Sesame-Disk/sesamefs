-- Migration 025: durable cleanup intent for repair-owned pub:<repo:commit:fsID>.
--
-- The published-block-reference repair worker renews a repair-owned
-- pub:<repo:commit:fsID> BEFORE its bounded reachability walk
-- (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). A writer's ordinary
-- settlement deletes only the repair row, so a clear that lands during that
-- walk can leave the just-written pin without a durable owner; the in-visit
-- compensation may then fail (per-block DELETE fan-out, read error, process
-- loss) with no row left for a sweep to rediscover. This table is the
-- write-ahead cleanup witness: a visit writes its intent before the pub:
-- write and does not renew if that write fails. One lifecycle for every
-- retirement (positive settlement, in-visit compensation, sweep):
--
--   tombstone the producer's pins at its lease (EACH_QUORUM)
--   -> RETIRE the intent to CONSUMED
--   -> periodic global re-fence (every 3 days, refenced_at advances only
--      after every DC acknowledged the tombstone)
--   -> at retention (35 days = pin TTL): one mandatory FINAL global fence
--   -> only after it succeeded: terminal DELETE (SERIAL IF EXISTS)
--
-- The sweep processes leftovers (repair row pending -> keep/compact, repair
-- row conclusively gone -> the lifecycle above). It never authorizes
-- promotion or repair-row deletion and never resurrects a repair row.
--
-- `producer_token` is minted per visit (one renewal producer), not per repair row:
-- concurrent visits of the same row and requeues of the same identity each
-- own a distinct witness, and every producer deletes only its own token.
--
-- `armed` / `lease_expires_at` are the producer<->sweeper fence, and every
-- transition of a row lives in ONE Paxos state machine (the INSERT is
-- IF NOT EXISTS; EXTEND, ARM and FREEZE are conditional LWTs), so the cells a
-- ballot commits are never shadowed by an ordinary write's wall-clock
-- timestamp:
--
--   PREPARING(L)  --EXTEND (IF armed=false AND lease=L)-->  PREPARING(L')
--   PREPARING(L)  --ARM    (IF armed=false)             -->  FINISHED(L)   [producer]
--   PREPARING(L)  --FREEZE (IF armed=false AND lease=L) -->  FINISHED(L)   [sweeper]
--
-- The intent is written PREPARING with a lease. The producer's per-block
-- pub: fan-out is NOT cut by that lease: before each block it EXTENDs the
-- lease once less than two skews remain, and every pin it writes carries
-- USING TIMESTAMP = the lease current at that write. An expired lease
-- therefore means only "this producer has not been seen extending"; it is
-- NOT cleanup authority by observation alone. A sweeper that lists an expired
-- PREPARING intent must CLAIM it with the FREEZE on the exact lease it
-- observed: FREEZE and EXTEND need the same exact lease and ARM needs
-- armed = false, so exactly one side wins. Won: the producer can no longer
-- extend or arm, every pin under this token carries a timestamp <= this
-- lease, and the intent is a finished producer. Lost: the producer extended
-- (or armed) after the listing; the stale observation authorizes nothing.
--
-- The sweep consumes only FINISHED intents (repair row conclusively gone ->
-- tombstone the pin at the intent's lease, RETIRE the intent) and compacts
-- FINISHED intents of a pending identity to the one with the greatest lease
-- (its eventual tombstone covers every discarded producer's pins) — frozen
-- producers included, so abandoned PREPARING intents do not accumulate.
--
-- Retirement is not deletion. A tombstone shadows a late write of the
-- producer only while it exists, and Cassandra purges it after
-- gc_grace_seconds (10 days by default on block_references); a producer
-- suspended between its pre-write lease check and its INSERT for longer
-- than that would land an ownerless pin. A retired witness therefore becomes
-- CONSUMED (`consumed_at`, `refenced_at`; SERIAL LWT IF armed = true AND
-- consumed_at = null) and the sweep re-tombstones its pins at the same lease
-- every 3 days (two intervals fit inside gc_grace, so one missed tick opens
-- no purge window) until 35 days (= the pin TTL) have passed. Every fence
-- tombstone is EACH_QUORUM and refenced_at advances only after it succeeded:
-- "refenced_at is fresh" means "the fence is physically in place in every
-- DC". At retention the sweep issues one mandatory FINAL fence and deletes
-- the witness (SERIAL IF EXISTS LWT) only if that fence succeeded — the
-- witness is the last cleanup root and never disappears on the strength of
-- a timestamp alone. Every transition of the row, including its terminal
-- disappearance, is therefore a Paxos CAS. The residual is a producer
-- suspended for longer than the pin it would write lives. block_references
-- gc_grace_seconds is pinned explicitly by migration 026 so the 3-day
-- interval is certified against the schema, not against a compiled default.
--
-- Rows carry NO TTL: the sweep bounds the intent's life, not the clock.

CREATE TABLE IF NOT EXISTS published_repair_liveness_cleanups (
    bucket           INT,
    org_id           UUID,
    repo_id          UUID,
    commit_id        TEXT,
    fs_id            TEXT,
    producer_token   TEXT,
    staged_block_ids LIST<TEXT>,
    created_at       TIMESTAMP,
    armed            BOOLEAN,
    lease_expires_at TIMESTAMP,
    consumed_at      TIMESTAMP,
    refenced_at      TIMESTAMP,
    PRIMARY KEY ((bucket), org_id, repo_id, commit_id, fs_id, producer_token)
) WITH default_time_to_live = 0;
