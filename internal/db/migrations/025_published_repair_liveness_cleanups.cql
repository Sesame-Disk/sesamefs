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
-- gone -> remove the pin, then delete the intent). It never authorizes
-- promotion or repair-row deletion and never resurrects a repair row.
--
-- `generation` is the hydrated repair row's created_at (the same generation
-- the reachability progress LWTs bind). A visit and the sweep delete only the
-- generation they hold, so a cleanup of an older generation can never delete
-- the witness a requeued visit of the same identity has just written.
--
-- Rows carry NO TTL: the renewal fan-out that follows the intent is not
-- time-bounded, so no time margin can guarantee the intent outlives the pin.
-- The sweep bounds the intent's life instead: once the repair row is gone the
-- pin is removed (a no-op if it already expired) and the intent is deleted.

CREATE TABLE IF NOT EXISTS published_repair_liveness_cleanups (
    bucket           INT,
    org_id           UUID,
    repo_id          UUID,
    commit_id        TEXT,
    fs_id            TEXT,
    generation       TIMESTAMP,
    staged_block_ids LIST<TEXT>,
    created_at       TIMESTAMP,
    PRIMARY KEY ((bucket), org_id, repo_id, commit_id, fs_id, generation)
) WITH default_time_to_live = 0;
