-- Migration 024: durable positive-reachability progress for published repairs.
--
-- The repair row is already the durable unit (TTL 0). These columns let the
-- cold-path classifier resume a walk from one SERIAL HEAD observation instead
-- of restarting at a moving live HEAD. They are not cleanup authority and must
-- not be written by the ordinary queue INSERT.

ALTER TABLE published_block_reference_repairs
    ADD IF NOT EXISTS reachability_anchor_head_commit_id TEXT;
ALTER TABLE published_block_reference_repairs
    ADD IF NOT EXISTS reachability_cursor_commit_id TEXT;
