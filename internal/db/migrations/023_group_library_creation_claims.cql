-- Migration 023: group_library_creation_claims
--
-- H1 (ISSUE-LIBRARY-INITIAL-HEAD-CONCURRENCY-01) made the three group-owned
-- library creation handlers preserve the library row instead of rolling it
-- back when InitializeLibraryFS returns an outcome that may already be
-- published (UNKNOWN, or an adopted HEAD not yet visible locally) — a 503
-- Retry-After is returned instead. Without a durable, single-owner record of
-- that attempt, a client's retry minted a brand-new library_id, leaving the
-- first one durable with no group share and letting retries accumulate
-- against the org's library quota.
--
-- One row is the durable authority for one logical create: the operation is
-- identified by (org, owner, group, name) — the group is part of the identity
-- so a same-named create for another group can never resume (and share) this
-- one — and the row fixes every parameter a retry must reproduce: the
-- library_id to finish, the share_id and created_at the group share is
-- written with (shares_by_group clusters on created_at, so a retry that used
-- a fresh timestamp would duplicate the projection row), and the storage
-- class the library was minted with.
--
-- The row is acquired with INSERT ... IF NOT EXISTS (Paxos decides exactly
-- one owner even for concurrent same-operation requests, and a blind
-- datacenter observes an existing claim through the same serial domain
-- instead of a local-quorum miss) and released only with
-- DELETE ... IF library_id = ? so an attempt can never clear another
-- attempt's claim. It is released once the group share is created or the
-- fresh library is definitively rolled back. There is deliberately NO TTL:
-- the preserved library the claim points at has no TTL either, and a claim
-- that expired first would let a late retry mint the very duplicate this
-- table exists to prevent. A claim whose library row is gone or trashed is
-- stale and is released (conditionally) by the next same-operation request.

CREATE TABLE IF NOT EXISTS group_library_creation_claims (
    org_id        UUID,
    owner_id      UUID,
    group_id      UUID,
    name          TEXT,
    library_id    UUID,
    share_id      UUID,
    storage_class TEXT,
    created_at    TIMESTAMP,
    PRIMARY KEY ((org_id, owner_id), group_id, name)
);
