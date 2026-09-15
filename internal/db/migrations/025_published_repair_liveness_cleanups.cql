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
-- it; the sweep processes leftovers (repair row pending -> keep, repair row
-- conclusively gone -> remove the pin, then delete the intent). It never
-- authorizes promotion or repair-row deletion and never resurrects a repair
-- row.
--
-- `producer_token` is minted per visit (one renewal producer), not per repair row:
-- concurrent visits of the same row and requeues of the same identity each
-- own a distinct witness, and every producer deletes only its own token.
--
-- `armed` / `lease_expires_at` fence the producer against the sweep. The
-- intent is written PREPARING (armed = false) with a lease; the producer's
-- per-block pub: fan-out is bounded to stop before that lease expires; once
-- the fan-out is done the producer ARMs the intent. The sweep may consume an
-- intent (repair row gone -> remove the pin, delete the intent) only when it
-- is armed, or when it is still preparing but its lease has expired -- i.e.
-- only once the producer can no longer write a pin the witness must cover.
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
