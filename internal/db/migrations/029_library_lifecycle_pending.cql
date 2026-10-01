-- ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01: durable continuation of library
-- lifecycle transitions (soft delete, restore, permanent delete).
--
-- A transition writes its row here before its canonical global-SERIAL LWT and
-- deletes it after its completion (counter adjustment, derived rows,
-- reconciliation request). If the process dies in between, the Server-owned
-- lifecycle reaper (independent of GC_ENABLED) finds the row and finishes the
-- transition once the canonical lifecycle has moved past the row's
-- precondition: the transition can then no longer apply, so the row is not
-- dropped while its LWT could still commit. It is discovery only; the canonical
-- LWT stays the only authority.
--
-- Fixed recovery buckets avoid a single global partition (same scheme as
-- library_rollback_pending). One row per transition: (operation, target_at)
-- identifies it, target_at being the transition's lifecycle value. No TTL.

CREATE TABLE IF NOT EXISTS library_lifecycle_pending (
    recovery_bucket   INT,
    org_id            UUID,
    library_id        UUID,
    operation         TEXT,
    target_at         TIMESTAMP,
    owner_id          UUID,
    prev_lifecycle_at TIMESTAMP,
    prev_deleted_at   TIMESTAMP,
    recorded_at       TIMESTAMP,
    PRIMARY KEY ((recovery_bucket), org_id, library_id, operation, target_at)
) WITH CLUSTERING ORDER BY (org_id ASC, library_id ASC, operation ASC, target_at ASC)
  AND default_time_to_live = 0;
