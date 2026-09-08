-- Migration 022: write-once G1 first_seen_at token for the durable D lifecycle.
--
-- StartBlockDeleteOrphan selects this value through the existing lifecycle
-- INSERT IF NOT EXISTS before publishing the ordinary recovery root. The root
-- remains free of LWT/SERIAL while concurrent callers reuse one token.

ALTER TABLE gc_block_delete_lifecycles ADD IF NOT EXISTS first_seen_at TIMESTAMP;
