-- A permanent-delete intent retains the authorization and read-model keys before
-- the canonical generation CAS. HTTP retries can complete cleanup with GC disabled.
ALTER TABLE deleted_libraries ADD owner_id UUID;
ALTER TABLE deleted_libraries ADD created_at TIMESTAMP;
