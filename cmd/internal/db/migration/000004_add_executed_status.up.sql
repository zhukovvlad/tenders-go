-- =====================================================================================
-- Migration 000004: Simplify suggested_merges resolution tracking
--
-- Previously the table had separate decided_at/decided_by (from 000001) for the
-- approval decision AND executed_at/executed_by for the actual merge execution.
-- Since merge execution is synchronous, we collapse both into a single pair:
--   resolved_at  – when the admin resolved (approved+executed or rejected)
--   resolved_by  – who resolved it
-- =====================================================================================

-- 1. Update CHECK constraint: add EXECUTED as a terminal state
ALTER TABLE suggested_merges
DROP CONSTRAINT "ck_suggested_merges_status";

ALTER TABLE suggested_merges
ADD CONSTRAINT "ck_suggested_merges_status"
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED', 'EXECUTED'));

-- 2. Drop the legacy decided_at / decided_by columns (added in 000001)
ALTER TABLE suggested_merges
DROP COLUMN IF EXISTS decided_at,
DROP COLUMN IF EXISTS decided_by;

-- 3. Add unified resolution columns
ALTER TABLE suggested_merges
ADD COLUMN resolved_at TIMESTAMPTZ,
ADD COLUMN resolved_by TEXT;
