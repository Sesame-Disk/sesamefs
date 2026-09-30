-- ISSUE-GC-HARD-DELETE-LEASE-NONFENCING-01: per-library lifecycle clock.
-- Every lifecycle LWT (soft delete, restore) advances it strictly: the new value
-- is max(now, previous + 1 ms), set inside the same conditional update. Soft
-- delete uses the new value as deleted_at, so trash generations are unique per
-- library even within one millisecond or across skewed clocks, and every
-- lifecycle transition stamps its derived writes (marker, read model) with it,
-- so they are ordered like the canonical transitions. NULL until the library's
-- first lifecycle transition.
ALTER TABLE libraries ADD IF NOT EXISTS lifecycle_at timestamp;
