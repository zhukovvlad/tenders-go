-- =====================================================================================
-- ФАЙЛ МИГРАЦИИ "ВНИЗ" (DOWN) ДЛЯ 000004_add_rag_workflow (Hardened v2)
-- Версия: 4 (Down)
--
-- Описание:
-- Этот скрипт ПОЛНОСТЬЮ ОТКАТЫВАЕТ изменения, внесенные миграцией "вверх".
--
-- Порядок выполнения (обратный "up"):
-- 1. Удалить VIEW "catalog_positions_clean".
-- 2. Удалить `suggested_merges` (включая ее индекс и CHECK).
-- 3. Удалить `matching_cache`.
-- 4. Модифицировать `catalog_positions`:
--    a. Удалить *новый* частичный HNSW-индекс.
--    b. *Восстановить* *старый* HNSW-индекс (из 000002).
--    c. Удалить `CHECK`-ограничение.
--    d. Удалить колонку `kind` и ее индексы.
-- =====================================================================================


-- --- ШАГ 1: Удаление VIEW ---
DROP VIEW IF EXISTS "catalog_positions_clean";


-- --- ШАГ 2: Удаление таблицы `suggested_merges` ---
-- (Индексы, CHECK-ограничения, комментарии и FK удаляются автоматически вместе с таблицей)
DROP TABLE IF EXISTS "suggested_merges";


-- --- ШАГ 3: Удаление таблицы `matching_cache` ---
-- (Индексы, комментарии и FK удаляются автоматически вместе с таблицей)
DROP TABLE IF EXISTS "matching_cache";


-- --- ШАГ 4: Откат изменений в `catalog_positions` ---

-- 4a. Удаляем *новый* частичный HNSW-индекс
DROP INDEX IF EXISTS "idx_cp_kind_pos_hnsw";

-- 4b. *Восстанавливаем* *старый* полный HNSW-индекс (из миграции 000002)
CREATE INDEX IF NOT EXISTS "catalog_positions_embedding_idx"
ON "catalog_positions" USING HNSW (embedding vector_cosine_ops);
COMMENT ON INDEX "catalog_positions_embedding_idx" IS 'Восстановленный HNSW-индекс из миграции 000002';

-- 4c. Удаляем CHECK-ограничение
ALTER TABLE "catalog_positions"
DROP CONSTRAINT IF EXISTS "ck_catalog_positions_kind";

-- 4d. Удаляем индексы колонки `kind`
DROP INDEX IF EXISTS "idx_catalog_positions_kind";
DROP INDEX IF EXISTS "idx_cp_kind_review";

-- 4e. Удаляем саму колонку `kind`
ALTER TABLE "catalog_positions"
DROP COLUMN IF EXISTS "kind";