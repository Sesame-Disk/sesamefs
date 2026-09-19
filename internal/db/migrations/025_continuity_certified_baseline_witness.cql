-- PC-D1A: explicit baseline continuity witness.
-- NULL means that no certified witness exists for the current HEAD.
ALTER TABLE libraries ADD IF NOT EXISTS continuity_certified_head_commit_id text;
ALTER TABLE libraries ADD IF NOT EXISTS continuity_contract_version text;
