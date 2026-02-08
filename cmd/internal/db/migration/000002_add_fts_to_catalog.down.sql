-- =====================================================================================
-- ОТКАТ МИГРАЦИИ: 000002_add_fts_to_catalog
-- =====================================================================================

-- 1. Удаляем индекс
DROP INDEX IF EXISTS "idx_catalog_positions_fts";

-- 2. Удаляем колонку
ALTER TABLE "catalog_positions" DROP COLUMN IF EXISTS "fts_vector";