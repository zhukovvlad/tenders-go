-- =====================================================================================
-- Migration 000004: Simplify suggested_merges resolution tracking
--
-- The original schema (000001) tracked approval decisions via decided_at/decided_by.
-- Since merge execution is synchronous with approval, we consolidate into a single
-- pair of columns:
--   resolved_at  – when the admin resolved (approved+executed or rejected)
--   resolved_by  – who resolved it
-- =====================================================================================

-- 1. Update CHECK constraint: add EXECUTED as a terminal state
ALTER TABLE suggested_merges
DROP CONSTRAINT "ck_suggested_merges_status";

ALTER TABLE suggested_merges
ADD CONSTRAINT "ck_suggested_merges_status"
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED', 'EXECUTED'));

-- 2. Add unified resolution columns
ALTER TABLE suggested_merges
ADD COLUMN resolved_at TIMESTAMPTZ,
ADD COLUMN resolved_by TEXT;

-- 3. Backfill from legacy columns (preserve existing decision metadata)
UPDATE suggested_merges
SET
    resolved_at = decided_at,
    resolved_by = decided_by
WHERE decided_at IS NOT NULL OR decided_by IS NOT NULL;

-- 4. Drop the legacy decided_at / decided_by columns (added in 000001)
ALTER TABLE suggested_merges
DROP COLUMN IF EXISTS decided_at,
DROP COLUMN IF EXISTS decided_by;
