-- Drop the old constraint
ALTER TABLE catalog_positions
DROP CONSTRAINT ck_catalog_positions_kind;

-- Add the updated constraint including 'GROUP_TITLE'
ALTER TABLE catalog_positions
ADD CONSTRAINT ck_catalog_positions_kind
CHECK (kind IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW', 'GROUP_TITLE'));
