-- Migration 026: certify block_references gc_grace_seconds.
--
-- The published-block-reference repair cleanup fences a retired producer's
-- late pub:<repo:commit:fsID> write with a tombstone at the producer lease and
-- re-issues that tombstone every 3 days for the 35-day pin TTL (migration
-- 025). That schedule is only sound while a tombstone lives at least two
-- intervals: publishedBlockReferenceRepairLivenessRefenceInterval (3d) x 2
-- <= gc_grace_seconds. Until now the table inherited the Cassandra default
-- (864000 s = 10 days) and the Go constant
-- publishedBlockReferenceRepairBlockReferencesGCGrace merely assumed it; a
-- deployment lowering gc_grace_seconds on this table would have silently
-- shortened the fence below the re-fence interval. Pin the value the code is
-- certified against. Changing it requires changing the constant and its guard
-- (TestPublishedBlockReferenceRepairRefenceIntervalBeatsGCGrace) together.

ALTER TABLE block_references WITH gc_grace_seconds = 864000;
