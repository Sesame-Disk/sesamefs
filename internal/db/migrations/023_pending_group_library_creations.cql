-- Migration 023: pending_group_library_creations
--
-- H1 (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01) made the three group-owned
-- library creation handlers preserve the library row instead of rolling it
-- back when InitializeLibraryFS returns an outcome that may already be
-- published (UNKNOWN, or an adopted HEAD not yet visible locally) — a 503
-- Retry-After is returned instead. Without a durable pointer back to that
-- preserved library, a client's retry minted a brand-new library_id, leaving
-- the first one durable with no group share and letting retries accumulate
-- against the org's library quota.
--
-- This table is a narrow, purpose-built idempotency marker: written in the
-- same batch as a fresh library's rows, keyed by (org_id, owner_id, name) so
-- a retry of the same create request finds the same library_id instead of
-- name-matching against libraries_by_owner (which would also match an
-- unrelated, intentionally-unshared personal library of the same name and
-- wrongly attach a group share to it). Cleared once the share is created
-- (success) or the library is rolled back (definitive failure). The TTL
-- bounds growth from attempts nobody ever retries; it does not affect the
-- preserved library row itself, which has no TTL.

CREATE TABLE IF NOT EXISTS pending_group_library_creations (
    org_id      UUID,
    owner_id    UUID,
    name        TEXT,
    library_id  UUID,
    group_id    UUID,
    created_at  TIMESTAMP,
    PRIMARY KEY ((org_id, owner_id), name)
) WITH default_time_to_live = 604800;
