-- Rollback: drop partial indexes for InvalidateRelatedPendingMerges

DROP INDEX IF EXISTS idx_suggested_merges_main_pos_actionable;
DROP INDEX IF EXISTS idx_suggested_merges_dup_pos_actionable;
