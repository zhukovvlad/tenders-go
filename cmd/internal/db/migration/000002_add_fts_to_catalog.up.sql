-- =====================================================================================
-- МИГРАЦИЯ: 000002_add_fts_to_catalog
-- Описание: Добавление инфраструктуры полнотекстового поиска (FTS) для RAG.
-- Использует конфигурацию 'simple', так как данные уже лемматизированы через spaCy.
-- =====================================================================================

-- 1. Добавляем генерируемую колонку fts_vector
-- Мы используем standard_job_title, так как это очищенные воркером данные
ALTER TABLE "catalog_positions" 
ADD COLUMN IF NOT EXISTS "fts_vector" tsvector 
GENERATED ALWAYS AS (
  to_tsvector('simple', coalesce("standard_job_title", ''))
) STORED;

-- 2. Создаем GIN-индекс для мгновенного поиска по тексту
-- Это критично для производительности RRF при больших объемах данных
CREATE INDEX IF NOT EXISTS "idx_catalog_positions_fts" 
ON "catalog_positions" USING GIN ("fts_vector");

-- 3. Обновляем статистику планировщика
ANALYZE "catalog_positions";