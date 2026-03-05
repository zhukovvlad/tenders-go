-- Revert Migration 000007: Remove Variants Grouping and JSONB Parameters

-- 1. Revert suggested_merges status constraint
ALTER TABLE suggested_merges
DROP CONSTRAINT ck_suggested_merges_status;

UPDATE suggested_merges
SET status = 'REJECTED'
WHERE status = 'GROUPED';

ALTER TABLE suggested_merges
ADD CONSTRAINT ck_suggested_merges_status
CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXECUTED'));

-- 2. Drop indexes
DROP INDEX IF EXISTS idx_catalog_positions_parameters_gin;
DROP INDEX IF EXISTS idx_catalog_positions_parent_id;

-- 3. Drop constraints
ALTER TABLE catalog_positions
DROP CONSTRAINT IF EXISTS chk_not_self_parent;

ALTER TABLE catalog_positions
DROP CONSTRAINT IF EXISTS fk_catalog_positions_parent;

-- 4. Drop columns
ALTER TABLE catalog_positions
DROP COLUMN IF EXISTS parameters,
DROP COLUMN IF EXISTS parent_id;
