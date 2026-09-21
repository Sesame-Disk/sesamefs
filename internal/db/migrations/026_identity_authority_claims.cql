-- PC-D1B: per-identity metadata authority claims.
--
-- A complete `commits` / `fs_objects` row is not proof that it is the
-- authoritative version of that identity: no shared write-once protocol covers
-- every writer, so two complete rows can disagree. This table is the durable
-- provenance the certified-baseline certifier needs before it may witness a
-- HEAD (ISSUE-PCD1B-METADATA-IDENTITY-AUTHORITY-01,
-- docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md).
--
-- Partition key is the FULL triple, not (library_id) with clustering columns.
-- `commits` and `fs_objects` are keyed ((library_id), id): claiming through a
-- library-wide partition would serialize every identity in a library into one
-- LWT partition. `block_id_mappings` already uses the full-triple layout for
-- the same reason.
--
-- `digest` is a versioned digest of the semantic projection that tree traversal
-- and dependency resolution consume, never of display or update metadata. The
-- claim is write-once: the first claim fixes the digest, an identical retry is
-- idempotent, and a different digest is a conflict.
--
-- No TTL, deliberately. The claim must outlive its source row so a deleted and
-- re-created key cannot mint fresh provenance (metadata ABA). The accepted cost
-- is that claims accumulate for failed initializations, losing commits and
-- deleted identities/libraries; retiring one is its own fenced protocol and is
-- tracked as ISSUE-PCD1B-AUTHORITY-CLAIM-RETIREMENT-01.
CREATE TABLE IF NOT EXISTS identity_authority_claims (
	library_id     UUID,
	identity_kind  TEXT,
	identity_id    TEXT,
	digest_version TEXT,
	digest         TEXT,
	created_at     TIMESTAMP,
	PRIMARY KEY ((library_id, identity_kind, identity_id))
);
