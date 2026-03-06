-- Drop the old constraint
ALTER TABLE catalog_positions
DROP CONSTRAINT ck_catalog_positions_kind;

-- Add the updated constraint including 'GROUP_TITLE'
ALTER TABLE catalog_positions
ADD CONSTRAINT ck_catalog_positions_kind
CHECK (kind IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW', 'GROUP_TITLE'));

-- Migrate legacy parent groups: HEADER parents → GROUP_TITLE
-- Only converts HEADER rows that are actually used as parent_id by other positions.
-- Also requeues active parents for embedding by the Python worker.
UPDATE catalog_positions
SET kind = 'GROUP_TITLE',
    description = COALESCE(description, standard_job_title),
    status = CASE
        WHEN status = 'active' THEN 'pending_indexing'
        ELSE status
    END
WHERE kind = 'HEADER'
  AND id IN (
    SELECT DISTINCT parent_id
    FROM catalog_positions
    WHERE parent_id IS NOT NULL
  );
