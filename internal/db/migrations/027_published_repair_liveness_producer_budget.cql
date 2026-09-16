-- Migration 027: bound durable liveness producers per repair generation.
--
-- A pending repair may be revisited after an unresolved classification, and
-- each visit owns a producer-token cleanup witness. The counter is a nullable
-- legacy-compatible, monotonic reservation count on the existing repair row.
-- Runtime reserves it with a SERIAL CAS bound to created_at before minting a
-- token; it is never decremented while the row is pending. A failed or
-- ambiguous follow-up intent write can therefore leak one slot, which is
-- deliberately fail-closed but cannot permit unbounded witness creation.

ALTER TABLE published_block_reference_repairs
    ADD IF NOT EXISTS liveness_producer_count INT;