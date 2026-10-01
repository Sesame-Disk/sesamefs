-- ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01: durable continuation of library
-- lifecycle transition attempts (soft delete, restore, permanent delete).
--
-- An attempt writes its row here (global QUORUM) before its canonical
-- global-SERIAL LWT and deletes its own row once it is settled: applied and
-- completed, or definitively not applied. If the process dies in between, the
-- Server-owned lifecycle reaper (independent of GC_ENABLED, reading at global
-- QUORUM) finds the row and finishes the attempt once the canonical lifecycle
-- has moved past the row's precondition, so a row is never dropped while its
-- LWT could still commit. An attempt that is still applicable after the
-- abandonment threshold (it died before its LWT, or its LWT failed without
-- applying) is first fenced by a Paxos transition that advances the
-- library's lifecycle clock under the same precondition. It is discovery only;
-- the canonical LWT stays the only authority.
--
-- Fixed recovery buckets avoid a single global partition (same scheme as
-- library_rollback_pending). One row per attempt: attempt_id identifies it, so
-- two attempts that propose the same transition (same operation and target_at,
-- the transition's lifecycle value) never share or delete each other's row.
-- No TTL: a row disappears only once its attempt can no longer apply.

CREATE TABLE IF NOT EXISTS library_lifecycle_pending (
    recovery_bucket   INT,
    org_id            UUID,
    library_id        UUID,
    operation         TEXT,
    target_at         TIMESTAMP,
    attempt_id        UUID,
    owner_id          UUID,
    prev_lifecycle_at TIMESTAMP,
    prev_deleted_at   TIMESTAMP,
    recorded_at       TIMESTAMP,
    PRIMARY KEY ((recovery_bucket), org_id, library_id, operation, target_at, attempt_id)
) WITH CLUSTERING ORDER BY (org_id ASC, library_id ASC, operation ASC, target_at ASC, attempt_id ASC)
  AND default_time_to_live = 0;
