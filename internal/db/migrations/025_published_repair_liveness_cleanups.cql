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
-- write and does not renew if that write fails; positive settlement deletes
-- it; the sweep processes leftovers (repair row pending -> keep/compact,
-- repair row conclusively gone -> remove the pin, then delete the intent). It
-- never authorizes promotion or repair-row deletion and never resurrects a
-- repair row.
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
-- tombstone the pin at the intent's lease, delete the intent) and compacts
-- FINISHED intents of a pending identity to the one with the greatest lease
-- (its eventual tombstone covers every discarded producer's pins) — frozen
-- producers included, so abandoned PREPARING intents do not accumulate.
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
    PRIMARY KEY ((bucket), org_id, repo_id, commit_id, fs_id, producer_token)
) WITH default_time_to_live = 0;
