-- 000007_add_unit_to_catalog.up.sql

-- 1. Добавляем колонку unit_id в таблицу каталога
ALTER TABLE "catalog_positions" 
ADD COLUMN "unit_id" bigint;

-- 2. Добавляем внешний ключ
ALTER TABLE "catalog_positions" 
ADD CONSTRAINT "catalog_positions_unit_id_fkey" 
FOREIGN KEY ("unit_id") REFERENCES "units_of_measurement" ("id") ON DELETE SET NULL;

COMMENT ON COLUMN "catalog_positions"."unit_id" IS 'Каноническая единица измерения. Часть уникального ключа позиции.';

-- 3. Удаляем старый индекс, который требовал уникальности только по названию
DROP INDEX IF EXISTS "uq_catalog_positions_std_job_title";

-- 4. Создаем новый индекс: Уникальность = Название + Единица измерения
-- Использование COALESCE нужно, чтобы Postgres считал NULL != NULL, 
-- но в рамках бизнес-логики два NULL'а считались дубликатом (если вдруг unit не задан).
CREATE UNIQUE INDEX "uq_catalog_positions_title_unit" 
ON "catalog_positions" ("standard_job_title", COALESCE("unit_id", -1));