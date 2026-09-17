-- Migration 025: durable cleanup witness for repair-owned
-- pub:<repo:commit:fsID>:<producer_token>, and the block_references
-- gc_grace_seconds that witness's fence schedule is certified against.
--
-- The published-block-reference repair worker renews a repair-owned pin
-- BEFORE its bounded reachability walk
-- (ISSUE-PUBLISH-REPAIR-RENEWAL-AFTER-CLASSIFY-01). A writer's ordinary
-- settlement deletes only the repair row, so a clear that lands during that
-- walk can leave the just-written pin without a durable owner; the in-visit
-- compensation may then fail (per-block DELETE fan-out, read error, process
-- loss) with no row left for a sweep to rediscover. This table is the
-- write-ahead cleanup witness: a visit writes its intent before the pin and
-- writes no pin if that write fails.
--
-- One row per PRODUCER: `producer_token` is a UUID minted per visit (one
-- renewal producer), not per repair row. Concurrent visits of the same row
-- and requeues of the same identity each own a distinct witness, and the
-- token is part of the physical referrer
-- (pub:<repo:commit:fsID>:<producer_token>), so every compensation, fence
-- and terminal delete reaches exactly one producer's pins and can never
-- touch another producer's — whatever their lease ordering.
--
-- `armed` / `lease_expires_at` are the producer<->sweeper fence, and every
-- transition of a row lives in ONE Paxos state machine (the INSERT is
-- IF NOT EXISTS; EXTEND, ARM, FREEZE, RETIRE, REFENCE and the terminal
-- DELETE are conditional SERIAL LWTs), so the cells a ballot commits are
-- never shadowed by an ordinary write's wall-clock timestamp:
--
--   PREPARING(L)  --EXTEND (IF armed=false AND lease=L)-->  PREPARING(L')
--   PREPARING(L)  --ARM    (IF armed=false)             -->  FINISHED(L)   [producer]
--   PREPARING(L)  --FREEZE (IF armed=false AND lease=L) -->  FINISHED(L)   [sweeper]
--   FINISHED(L)   --RETIRE (IF armed=true AND consumed_at=null)
--                                                       -->  CONSUMED(L)
--   CONSUMED(L)   --REFENCE (IF consumed_at=? AND refenced_at=?)
--                                                       -->  CONSUMED(L)   [refenced_at advances]
--   CONSUMED(L)   --DELETE IF EXISTS (after the final fence)--> gone
--
-- The intent is written PREPARING with a lease. The producer's per-block
-- pin fan-out is NOT cut by that lease: before each block it EXTENDs the
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
-- What the sweep does with a FINISHED witness depends only on the repair
-- row, decided at EACH_QUORUM (a local absence never authorizes anything):
--
--   repair row conclusively gone
--     -> tombstone the producer's pins at its lease (EACH_QUORUM)
--     -> RETIRE the intent to CONSUMED
--     -> periodic global re-fence (every 3 days; refenced_at advances only
--        after a quorum in every DC acknowledged the tombstone)
--     -> at retention (35 days = pin TTL): one mandatory FINAL global fence
--     -> only after it succeeded: terminal DELETE (SERIAL IF EXISTS)
--
--   repair row pending
--     -> RETAIN every FINISHED witness; no destructive compaction.
--        A FINISHED witness does not prove that every staged block was
--        pinned: ARM can follow a partial fan-out failure and FREEZE can
--        claim a producer abandoned mid fan-out, and with the token in the
--        physical referrer no other producer's tombstone covers its pins.
--        Reuse or recycling of a producer is only safe behind a durable
--        full-coverage proof and is not part of this schema.
--
-- Lifetime. Rows carry NO TTL. A CONSUMED witness has a finite lifetime
-- bounded by the sweep and the retention above. A FINISHED witness of an
-- identity that stays PENDING currently has NO finite lifetime: every
-- unresolved visit adds one producer, and they are all retained until the
-- identity is conclusively gone (bounded reuse is a documented follow-up).
--
-- Retirement is not deletion. A tombstone shadows a late write of the
-- producer only while it exists, and Cassandra purges it after
-- gc_grace_seconds; a producer suspended between its pre-write lease check
-- and its INSERT for longer than that would land an ownerless pin. The
-- re-fence rounds keep a live tombstone at the lease for the whole
-- retention, which equals the pin TTL, so the residual is a producer
-- suspended for longer than the pin it would write lives. That schedule is
-- only sound while a tombstone lives at least two intervals:
-- publishedBlockReferenceRepairLivenessRefenceInterval (3d) x 2 <=
-- gc_grace_seconds of block_references. The table inherited the Cassandra
-- default (864000 s = 10 days) and the Go constant
-- publishedBlockReferenceRepairBlockReferencesGCGrace merely assumed it;
-- pin the value the code is certified against. Changing it requires
-- changing the constant and its guard
-- (TestPublishedBlockReferenceRepairRefenceIntervalBeatsGCGrace) together.
-- EACH_QUORUM is a quorum per DC, not every replica: the certification
-- above is for the RF=1-per-DC topology the 3-DC evidence runs; with RF>1 a
-- replica that missed a fence can resurrect a pin once the other replicas
-- purge the tombstone (over-retention only) unless repair runs inside
-- gc_grace_seconds (ISSUE-BLOCK-REFERENCES-RF-TOMBSTONE-CERTIFICATION-01).
--
-- The sweep never authorizes promotion or repair-row deletion and never
-- resurrects a repair row.

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

ALTER TABLE block_references WITH gc_grace_seconds = 864000;
