-- Migration 023: durable discovery marker for new-library rollback cleanup.
--
-- rollbackNewLibrary takes authority with DELETE FROM libraries ... IF
-- head_commit_id = null before tearing down derived rows. If the process dies
-- (or the cleanup batch fails) after that LWT applies, the canonical row is
-- gone while libraries_by_id, active owner/org/global projections, policies,
-- fs_objects and commits remain. This table makes that cleanup discoverable
-- across restart without becoming a second authority: the HEAD LWT remains
-- the only gate. The marker is recovery/discovery, never permission to
-- destroy a library whose HEAD another initializer published.
--
-- Fixed recovery buckets avoid a single global partition and allow a
-- paginated bounded scan. Logical identity is one pending rollback per
-- (org_id, library_id); the bucket is not part of that identity.
-- No TTL: pending work must remain recoverable until settlement.

CREATE TABLE IF NOT EXISTS library_rollback_pending (
    recovery_bucket INT,
    org_id          UUID,
    library_id      UUID,
    owner_id        UUID,
    created_at      TIMESTAMP,
    recorded_at     TIMESTAMP,
    PRIMARY KEY ((recovery_bucket), org_id, library_id)
) WITH CLUSTERING ORDER BY (org_id ASC, library_id ASC)
  AND default_time_to_live = 0;
