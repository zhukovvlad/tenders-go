-- 000007_add_unit_to_catalog.down.sql

-- ВНИМАНИЕ: Откат этой миграции приведет к потере данных о единицах измерения!
-- Если данные были созданы с использованием unit_id, они будут безвозвратно потеряны.

-- 1. Удаляем новый составной индекс (Название + Единица измерения с COALESCE)
DROP INDEX IF EXISTS "uq_catalog_positions_title_unit";

-- 2. Пытаемся восстановить старый индекс (Только Название)
-- КРИТИЧЕСКОЕ ОГРАНИЧЕНИЕ: Если в таблице есть дубликаты по standard_job_title
-- (которые были разрешены благодаря разным unit_id), этот шаг УПАДЕТ с ошибкой.
-- Решение: Перед откатом нужно вручную удалить/объединить дублирующиеся записи.
CREATE UNIQUE INDEX "uq_catalog_positions_std_job_title" 
ON "catalog_positions" ("standard_job_title");

-- 3. Явно удаляем внешний ключ перед удалением колонки (безопаснее, чем полагаться на CASCADE)
ALTER TABLE "catalog_positions" 
DROP CONSTRAINT IF EXISTS "catalog_positions_unit_id_fkey";

-- 4. Удаляем колонку unit_id
-- ПОТЕРЯ ДАННЫХ: Все связи с единицами измерения будут удалены!
ALTER TABLE "catalog_positions" 
DROP COLUMN IF EXISTS "unit_id";