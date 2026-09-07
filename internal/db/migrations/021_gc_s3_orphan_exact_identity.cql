-- Migration 021: exact (P,D) orphan identity and durable recovery root.
--
-- PRE-PRODUCTION SCAFFOLDING, NOT A BROWNFIELD UPGRADE PATH. The target
-- deployment is greenfield: destructive GC has never been activated there and
-- no durable orphan rows exist that must be preserved. GC_ENABLED=false today
-- is not that historical proof by itself; the deployment invariant is recorded
-- in docs/GC-X2-MULTIDC-VALIDATION.md and the production readiness record.
--
-- Cassandra cannot add clustering columns to the existing orphan tables. The
-- clean deployment premise therefore permits DROP + CREATE here, as migration
-- 018 does for the exact candidate identity. Once X1 closes, this shape is
-- folded into the initial schema for a clean production rollout.
--
-- P = (storage_class, storage_key)
-- D = (gc_claim_id, gc_claimed_at)
-- The recovery root is an enumeration surface only. Canonical recovery state
-- remains authoritative; a root row never authorizes a physical delete.

DROP TABLE IF EXISTS gc_s3_orphans_by_day;
DROP TABLE IF EXISTS gc_s3_orphans;

CREATE TABLE IF NOT EXISTS gc_s3_orphans (
    org_id          UUID,
    block_id        TEXT,
    storage_class   TEXT,
    storage_key     TEXT,
    gc_claim_id     TEXT,
    gc_claimed_at   TIMESTAMP,
    first_seen_at   TIMESTAMP,
    last_attempt_at TIMESTAMP,
    retry_count     INT,
    last_error      TEXT,
    external_sha1   TEXT,
    recovery_phase  TEXT,
    recovery_state  TEXT,
    PRIMARY KEY ((org_id, block_id), storage_class, storage_key, gc_claim_id, gc_claimed_at)
) WITH default_time_to_live = 0;

-- Non-authoritative scheduling projection. Every identity column is part of
-- the key, so the projection cannot carry an ambiguous prefix or mutable state.
CREATE TABLE IF NOT EXISTS gc_s3_orphans_by_day (
    first_seen_day DATE,
    bucket         INT,
    first_seen_at  TIMESTAMP,
    org_id         UUID,
    block_id       TEXT,
    storage_class  TEXT,
    storage_key    TEXT,
    gc_claim_id    TEXT,
    gc_claimed_at  TIMESTAMP,
    PRIMARY KEY ((first_seen_day, bucket), first_seen_at, org_id, block_id,
                 storage_class, storage_key, gc_claim_id, gc_claimed_at)
) WITH CLUSTERING ORDER BY (
    first_seen_at ASC,
    org_id ASC,
    block_id ASC,
    storage_class ASC,
    storage_key ASC,
    gc_claim_id ASC,
    gc_claimed_at ASC
)
  AND default_time_to_live = 0;

-- Independent, non-expiring restart root. Fixed buckets deliberately avoid a
-- time horizon: cold start scans every bucket and can find identities older
-- than the former 90-day TTL. The root carries no lifecycle state.
CREATE TABLE IF NOT EXISTS gc_s3_orphan_recovery_roots (
    root_bucket   INT,
    gc_claimed_at TIMESTAMP,
    org_id        UUID,
    block_id      TEXT,
    storage_class TEXT,
    storage_key   TEXT,
    gc_claim_id   TEXT,
    created_at    TIMESTAMP,
    first_seen_at TIMESTAMP,
    PRIMARY KEY ((root_bucket), gc_claimed_at, org_id, block_id,
                 storage_class, storage_key, gc_claim_id)
) WITH CLUSTERING ORDER BY (
    gc_claimed_at ASC,
    org_id ASC,
    block_id ASC,
    storage_class ASC,
    storage_key ASC,
    gc_claim_id ASC
)
  AND default_time_to_live = 0;
