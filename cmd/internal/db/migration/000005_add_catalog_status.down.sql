-- 000005_add_catalog_status.down.sql
-- Откатывает миграцию, удаляя колонку 'status'

-- Ограничение chk_catalog_positions_status и индексы
-- (idx_cp_status_pending, idx_cp_status)
-- удаляются автоматически, так как они зависят от колонки status.

ALTER TABLE "catalog_positions"
DROP COLUMN IF EXISTS "status";