-- Файл: 000002_add_rules_and_indexes.down.sql

-- --- ШАГ 1: Удаление индексов производительности ---
DROP INDEX IF EXISTS idx_gin_lots_key_parameters;
DROP INDEX IF EXISTS "catalog_positions_embedding_idx";
DROP INDEX IF EXISTS "lots_chunks_embedding_idx";


-- --- ШАГ 2: Возвращение внешних ключей к состоянию по умолчанию (ON DELETE RESTRICT) ---

ALTER TABLE "tenders" DROP CONSTRAINT "tenders_category_id_fkey";
ALTER TABLE "tenders" ADD CONSTRAINT "tenders_category_id_fkey" 
  FOREIGN KEY ("category_id") REFERENCES "tender_categories" ("id");

ALTER TABLE "tender_chapters" DROP CONSTRAINT "tender_chapters_tender_type_id_fkey";
ALTER TABLE "tender_chapters" ADD CONSTRAINT "tender_chapters_tender_type_id_fkey" 
  FOREIGN KEY ("tender_type_id") REFERENCES "tender_types" ("id");

ALTER TABLE "tender_categories" DROP CONSTRAINT "tender_categories_tender_chapter_id_fkey";
ALTER TABLE "tender_categories" ADD CONSTRAINT "tender_categories_tender_chapter_id_fkey" 
  FOREIGN KEY ("tender_chapter_id") REFERENCES "tender_chapters" ("id");

ALTER TABLE "lots_md_documents" DROP CONSTRAINT "lots_md_documents_lot_id_fkey";
ALTER TABLE "lots_md_documents" ADD CONSTRAINT "lots_md_documents_lot_id_fkey"
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id");

ALTER TABLE "proposals" DROP CONSTRAINT "proposals_lot_id_fkey";
ALTER TABLE "proposals" ADD CONSTRAINT "proposals_lot_id_fkey"
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id");

ALTER TABLE "lots_chunks" DROP CONSTRAINT "lots_chunks_lot_document_id_fkey";
ALTER TABLE "lots_chunks" ADD CONSTRAINT "lots_chunks_lot_document_id_fkey"
  FOREIGN KEY ("lot_document_id") REFERENCES "lots_md_documents" ("id");