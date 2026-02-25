-- =====================================================================================
-- Rollback Migration 000004
-- WARNING: converts EXECUTED → APPROVED and restores the old decided_at/decided_by
--          columns. Resolution metadata (resolved_at/by) is lost.
-- =====================================================================================

-- 1. Convert EXECUTED rows back to APPROVED so the old constraint is valid
UPDATE suggested_merges SET status = 'APPROVED' WHERE status = 'EXECUTED';

-- 2. Drop the unified resolution columns
ALTER TABLE suggested_merges
DROP COLUMN IF EXISTS resolved_at,
DROP COLUMN IF EXISTS resolved_by;

-- 3. Restore legacy decided_at / decided_by columns (were in 000001)
ALTER TABLE suggested_merges
ADD COLUMN decided_at TIMESTAMPTZ,
ADD COLUMN decided_by TEXT;

-- 4. Restore original CHECK constraint (without EXECUTED)
ALTER TABLE suggested_merges
DROP CONSTRAINT "ck_suggested_merges_status";

ALTER TABLE suggested_merges
ADD CONSTRAINT "ck_suggested_merges_status"
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED'));
