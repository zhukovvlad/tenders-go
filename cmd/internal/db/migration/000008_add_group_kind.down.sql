-- Revert GROUP_TITLE rows back to HEADER before re-adding the constraint
UPDATE catalog_positions SET kind = 'HEADER' WHERE kind = 'GROUP_TITLE';

-- Revert the constraint to exclude 'GROUP_TITLE'
ALTER TABLE catalog_positions
DROP CONSTRAINT ck_catalog_positions_kind;

ALTER TABLE catalog_positions
ADD CONSTRAINT ck_catalog_positions_kind
CHECK (kind IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW'));
