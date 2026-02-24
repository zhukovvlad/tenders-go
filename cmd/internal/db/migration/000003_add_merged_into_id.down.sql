-- Удаление колонки автоматически удалит и внешний ключ, и индекс, и constraint.
ALTER TABLE catalog_positions 
DROP COLUMN IF EXISTS merged_into_id;