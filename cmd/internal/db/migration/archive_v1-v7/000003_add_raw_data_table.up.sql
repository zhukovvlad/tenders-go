-- ФАЙЛ МИГРАЦИИ: 000003_add_raw_data_table.up.sql
-- Описание:
-- Добавляет таблицу для хранения исходных JSON-данных, полученных от парсера.
-- Это служит "источником правды" для отладки, аудита и повторной обработки.

CREATE TABLE "tender_raw_data" (
  "tender_id" BIGINT PRIMARY KEY,
  "raw_data" JSONB NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT (now())
);

COMMENT ON TABLE "tender_raw_data" IS 'Хранит исходный JSON, полученный от парсера. Обновляется при повторной загрузке тендера.';
COMMENT ON COLUMN "tender_raw_data"."tender_id" IS 'ID тендера. Является первичным и внешним ключом для связи 1-к-1.';

-- Добавляем внешний ключ с каскадным удалением.
-- Если тендер будет удален, связанный с ним JSON также удалится.
ALTER TABLE "tender_raw_data" ADD CONSTRAINT "tender_raw_data_tender_id_fkey"
  FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON DELETE CASCADE;
