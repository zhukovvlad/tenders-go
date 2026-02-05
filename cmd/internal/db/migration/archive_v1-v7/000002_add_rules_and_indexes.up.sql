-- =====================================================================================
-- ФАЙЛ МИГРАЦИИ ДЛЯ ДОБАВЛЕНИЯ ПРАВИЛ И ИНДЕКСОВ
-- Версия: 2
-- Описание:
-- Этот скрипт модифицирует существующую схему, добавляя:
-- 1.  ПРАВИЛА УДАЛЕНИЯ (ON DELETE): Определяет каскадное удаление (CASCADE)
--     или установку в NULL для связанных записей, обеспечивая целостность данных.
-- 2.  ИНДЕКСЫ ПРОИЗВОДИТЕЛЬНОСТИ: Добавляет специализированные индексы
--     для колонок типа `vector` и `jsonb` для ускорения поиска.
-- =====================================================================================


-- --- ШАГ 1: Обновление внешних ключей с правилами ON DELETE ---

-- Логика: Если категория удаляется, тендер не удаляется, а его category_id становится NULL.
ALTER TABLE "tenders" DROP CONSTRAINT IF EXISTS "tenders_category_id_fkey";
ALTER TABLE "tenders" ADD CONSTRAINT "tenders_category_id_fkey" 
  FOREIGN KEY ("category_id") REFERENCES "tender_categories" ("id") ON DELETE SET NULL;

-- Логика: Если удаляется родительская сущность в иерархии справочников, дочерние тоже удаляются.
ALTER TABLE "tender_chapters" DROP CONSTRAINT IF EXISTS "tender_chapters_tender_type_id_fkey";
ALTER TABLE "tender_chapters" ADD CONSTRAINT "tender_chapters_tender_type_id_fkey" 
  FOREIGN KEY ("tender_type_id") REFERENCES "tender_types" ("id") ON DELETE CASCADE;

ALTER TABLE "tender_categories" DROP CONSTRAINT IF EXISTS "tender_categories_tender_chapter_id_fkey";
ALTER TABLE "tender_categories" ADD CONSTRAINT "tender_categories_tender_chapter_id_fkey" 
  FOREIGN KEY ("tender_chapter_id") REFERENCES "tender_chapters" ("id") ON DELETE CASCADE;

-- Логика: Если удаляется лот, его документы и предложения также должны быть удалены.
ALTER TABLE "lots_md_documents" DROP CONSTRAINT IF EXISTS "lots_md_documents_lot_id_fkey";
ALTER TABLE "lots_md_documents" ADD CONSTRAINT "lots_md_documents_lot_id_fkey"
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id") ON DELETE CASCADE;

ALTER TABLE "proposals" DROP CONSTRAINT IF EXISTS "proposals_lot_id_fkey";
ALTER TABLE "proposals" ADD CONSTRAINT "proposals_lot_id_fkey"
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id") ON DELETE CASCADE;

-- Логика: Если удаляется документ, все его чанки также удаляются.
ALTER TABLE "lots_chunks" DROP CONSTRAINT IF EXISTS "lots_chunks_lot_document_id_fkey";
ALTER TABLE "lots_chunks" ADD CONSTRAINT "lots_chunks_lot_document_id_fkey"
  FOREIGN KEY ("lot_document_id") REFERENCES "lots_md_documents" ("id") ON DELETE CASCADE;


-- --- ШАГ 2: Создание индексов для производительности ---

-- Индекс для JSONB-колонки с ключевыми параметрами лотов
CREATE INDEX IF NOT EXISTS idx_gin_lots_key_parameters ON lots USING GIN (lot_key_parameters);

-- HNSW-индексы для векторного поиска (косинусное расстояние)
CREATE INDEX IF NOT EXISTS catalog_positions_embedding_idx ON catalog_positions USING HNSW (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS lots_chunks_embedding_idx ON lots_chunks USING HNSW (embedding vector_cosine_ops);