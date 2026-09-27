-- PC-D1B.5: certification-window lifecycle fence state lives on the
-- canonical library row so it shares the global SERIAL HEAD Paxos domain.
ALTER TABLE libraries ADD IF NOT EXISTS continuity_destruction_epoch timeuuid;
ALTER TABLE libraries ADD IF NOT EXISTS continuity_destruction_pending map<uuid, timeuuid>;
ALTER TABLE libraries ADD IF NOT EXISTS continuity_destruction_superseded timeuuid;
