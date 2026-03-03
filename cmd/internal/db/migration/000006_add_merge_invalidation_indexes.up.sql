-- =====================================================================================
-- Migration 000006: Add partial indexes for InvalidateRelatedPendingMerges
--
-- The InvalidateRelatedPendingMerges query filters by
--   status IN ('PENDING', 'APPROVED') AND (main_position_id = ANY(...) OR duplicate_position_id = ANY(...))
--
-- Without indexes on main_position_id/duplicate_position_id for actionable statuses,
-- PostgreSQL would do a sequential scan on suggested_merges, which degrades as the
-- table grows. This migration adds two partial indexes covering both lookup paths.
-- =====================================================================================

CREATE INDEX idx_suggested_merges_main_pos_actionable
ON suggested_merges (main_position_id)
WHERE status IN ('PENDING', 'APPROVED');

CREATE INDEX idx_suggested_merges_dup_pos_actionable
ON suggested_merges (duplicate_position_id)
WHERE status IN ('PENDING', 'APPROVED');
