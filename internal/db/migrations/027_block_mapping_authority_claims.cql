-- PC-D1B.3: durable Mapping Authority for external SHA-1 -> canonical SHA-256.
--
-- `block_id_mappings` is an operational, mutable projection. Its row existing,
-- or every replica agreeing on it, is not proof. The desktop sync upload path
-- accepts a client-asserted SHA-1. Writers do a plain read-before-write INSERT
-- with no LWT, so two same-key writers can both read "absent" and the later
-- INSERT wins. A delayed or in-flight mutation, including a hint, can land after
-- any read. This table is the separate, write-once authority that the
-- certified-baseline certifier consumes for SHA-1-only file dependencies
-- (docs/PC-D1B-METADATA-IDENTITY-AUTHORITY.md, PC-D1B.3). Promotion also
-- freezes the ordinary row to the claim so later ordinary writes are inert.
--
-- The key is exactly the `block_id_mappings` identity
-- ((org_id, representation_id, external_id)), so one mapping identity has one
-- Paxos partition and never shares an LWT domain with any other mapping.
--
-- A claim is written only by the cold-path promotion after independent
-- provenance: the canonical block belongs to the claimed representation and
-- its stored bytes hash to both internal_id (SHA-256) and external_id (SHA-1).
-- `evidence` records the provenance rule (only `physical_bytes_v1` is usable). Claims are INSERT ... IF NOT EXISTS under global SERIAL: the
-- first claim fixes internal_id, an identical retry is idempotent and a
-- different value is a conflict. Nothing updates or deletes a claim.
--
-- No TTL, deliberately, and the claim is independent of the mutable row:
-- rewriting a `block_id_mappings` row must never retire or change the
-- authority. Production never deletes `block_id_mappings` rows (R11a). Retirement is a separate future protocol
-- (ISSUE-PCD1B-AUTHORITY-CLAIM-RETIREMENT-01).
CREATE TABLE IF NOT EXISTS block_mapping_authority_claims (
	org_id            UUID,
	representation_id TEXT,
	external_id       TEXT,
	internal_id       TEXT,
	contract_version  TEXT,
	evidence          TEXT,
	created_at        TIMESTAMP,
	PRIMARY KEY ((org_id, representation_id, external_id))
);
