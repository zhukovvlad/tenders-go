-- =====================================================================================
-- ФАЙЛ ПЕРВОНАЧАЛЬНОЙ МИГРАЦИИ (ВЕРСИЯ 1)
-- Описание:
-- Этот скрипт создает полную базовую структуру таблиц для системы анализа тендеров.
-- Схема нормализована для обеспечения целостности данных и гибкости.
--
-- Ключевые архитектурные решения:
-- 1.  ИЕРАРХИЯ СПРАВОЧНИКОВ: Создана трехуровневая система классификации
--     (tender_types -> tender_chapters -> tender_categories) для гибкой аналитики.
-- 2.  СТРУКТУРИРОВАННЫЕ ПАРАМЕТРЫ: Таблица `lots` содержит поле `lot_key_parameters` (JSONB)
--     для хранения уникальных технических требований по каждому лоту.
-- 3.  ДАННЫЕ ДЛЯ RAG: Для семантического поиска по текстовой документации созданы
--     таблицы `lots_md_documents` (для исходных файлов) и `lots_chunks` (для чанков).
-- 4.  КАТАЛОГ РАБОТ: Таблица `catalog_positions` служит стандартизированным
--     справочником "сути" работ, что позволяет сравнивать одинаковые работы
--     в разных тендерах.
-- 5.  PGVECTOR: Схема с самого начала включает поддержку типа `vector` для хранения
--     эмбеддингов непосредственно в PostgreSQL.
--
-- ПРИМЕЧАНИЕ: Внешние ключи в этой миграции создаются без правил поведения
-- при удалении (ON DELETE). Эти правила будут добавлены в следующей миграции.
-- =====================================================================================

-- ШАГ 1: АКТИВАЦИЯ РАСШИРЕНИЯ
-- Эта команда должна быть самой первой, до создания любых таблиц,
-- использующих тип 'vector'.
CREATE EXTENSION IF NOT EXISTS vector;

-- --- Таблицы иерархических справочников ---

CREATE TABLE "tender_types" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);
COMMENT ON TABLE "tender_types" IS 'Верхний уровень иерархии (напр., "Строительство", "Проектирование")';

CREATE TABLE "tender_chapters" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "tender_type_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);
COMMENT ON TABLE "tender_chapters" IS 'Средний уровень иерархии, главы внутри типов (напр., "Общестроительные работы")';

CREATE TABLE "tender_categories" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "tender_chapter_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);
COMMENT ON TABLE "tender_categories" IS 'Нижний уровень иерархии, категории внутри глав (напр., "Нулевой цикл")';


-- --- Основные сущности: Тендеры, Лоты, Объекты, Заказчики ---

CREATE TABLE "tenders" (
  "id" BIGSERIAL PRIMARY KEY,
  "etp_id" varchar UNIQUE NOT NULL,
  "title" varchar NOT NULL,
  "category_id" bigint,
  "object_id" bigint NOT NULL,
  "executor_id" bigint NOT NULL,
  "data_prepared_on_date" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "objects" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "address" varchar NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "executors" (
  "id" BIGSERIAL PRIMARY KEY,
  "name" varchar UNIQUE NOT NULL,
  "phone" varchar NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "lots" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_key" varchar NOT NULL,
  "lot_title" varchar NOT NULL,
  "lot_key_parameters" jsonb,
  "tender_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);
COMMENT ON COLUMN "lots"."lot_key_parameters" IS 'Ключевые технические параметры, заданные заказчиком для этого лота';


-- --- Сущности, связанные с подрядчиками и их предложениями ---

CREATE TABLE "contractors" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar NOT NULL,
  "inn" varchar UNIQUE NOT NULL,
  "address" varchar NOT NULL,
  "accreditation" varchar NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "persons" (
  "id" BIGSERIAL PRIMARY KEY,
  "name" varchar NOT NULL,
  "phone" varchar NOT NULL,
  "email" varchar,
  "contractor_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "proposals" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_id" bigint NOT NULL,
  "contractor_id" bigint NOT NULL,
  "is_baseline" boolean NOT NULL DEFAULT false,
  "contractor_coordinate" varchar(255),
  "contractor_width" int,
  "contractor_height" int,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "winners" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint UNIQUE NOT NULL,
  "rank" int,
  "awarded_share" numeric(5,2),
  "notes" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);


-- --- Таблицы детализации предложений ---

CREATE TABLE "proposal_additional_info" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint NOT NULL,
  "info_key" text NOT NULL,
  "info_value" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "proposal_summary_lines" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint NOT NULL,
  "summary_key" text NOT NULL,
  "job_title" text NOT NULL,
  "materials_cost" numeric,
  "works_cost" numeric,
  "indirect_costs_cost" numeric,
  "total_cost" numeric,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "position_items" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint NOT NULL,
  "catalog_position_id" bigint NOT NULL,
  "position_key_in_proposal" varchar(255) NOT NULL,
  "comment_organazier" text,
  "comment_contractor" text,
  "item_number_in_proposal" varchar(50),
  "chapter_number_in_proposal" varchar(50),
  "job_title_in_proposal" text NOT NULL,
  "unit_id" bigint,
  "quantity" numeric,
  "suggested_quantity" numeric,
  "total_cost_for_organizer_quantity" numeric,
  "unit_cost_materials" numeric,
  "unit_cost_works" numeric,
  "unit_cost_indirect_costs" numeric,
  "unit_cost_total" numeric,
  "total_cost_materials" numeric,
  "total_cost_works" numeric,
  "total_cost_indirect_costs" numeric,
  "total_cost_total" numeric,
  "deviation_from_baseline_cost" numeric,
  "is_chapter" boolean NOT NULL DEFAULT false,
  "chapter_ref_in_proposal" varchar(50),
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);


-- --- Таблицы для RAG и семантического поиска ---

CREATE TABLE "catalog_positions" (
  "id" BIGSERIAL PRIMARY KEY,
  "standard_job_title" text NOT NULL,
  "description" text,
  "embedding" vector(768),
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "lots_md_documents" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_id" bigint NOT NULL,
  "document_name" varchar(255) NOT NULL,
  "full_content" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "lots_chunks" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_document_id" bigint NOT NULL,
  "chunk_index" int NOT NULL,
  "chunk_text" text NOT NULL,
  "embedding" vector(768) NOT NULL,
  "metadata" jsonb,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);


-- --- Справочник единиц измерения ---

CREATE TABLE "units_of_measurement" (
  "id" BIGSERIAL PRIMARY KEY,
  "normalized_name" varchar(50) UNIQUE NOT NULL,
  "full_name" text,
  "description" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);


-- --- Создание индексов для ускорения запросов ---

CREATE UNIQUE INDEX ON "lots" ("tender_id", "lot_key");
CREATE UNIQUE INDEX "uq_proposals_lot_contractor" ON "proposals" ("lot_id", "contractor_id");
CREATE UNIQUE INDEX "uq_proposal_additional_info_key" ON "proposal_additional_info" ("proposal_id", "info_key");
CREATE UNIQUE INDEX "uq_proposal_summary_lines_key" ON "proposal_summary_lines" ("proposal_id", "summary_key");
CREATE UNIQUE INDEX "uq_catalog_positions_std_job_title" ON "catalog_positions" ("standard_job_title");
CREATE UNIQUE INDEX "uq_position_items_proposal_id_key" ON "position_items" ("proposal_id", "position_key_in_proposal");
CREATE UNIQUE INDEX "uq_chunk_index_per_document" ON "lots_chunks" ("lot_document_id", "chunk_index");

CREATE INDEX "idx_position_items_proposal_id" ON "position_items" ("proposal_id");
CREATE INDEX "idx_position_items_catalog_id" ON "position_items" ("catalog_position_id");
CREATE INDEX "idx_position_items_unit_id" ON "position_items" ("unit_id");


-- --- Создание внешних ключей для обеспечения целостности данных ---

ALTER TABLE "tenders" ADD FOREIGN KEY ("category_id") REFERENCES "tender_categories" ("id");
ALTER TABLE "tenders" ADD FOREIGN KEY ("object_id") REFERENCES "objects" ("id");
ALTER TABLE "tenders" ADD FOREIGN KEY ("executor_id") REFERENCES "executors" ("id");

ALTER TABLE "tender_chapters" ADD FOREIGN KEY ("tender_type_id") REFERENCES "tender_types" ("id");
ALTER TABLE "tender_categories" ADD FOREIGN KEY ("tender_chapter_id") REFERENCES "tender_chapters" ("id");

ALTER TABLE "lots" ADD FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id");
ALTER TABLE "lots_md_documents" ADD FOREIGN KEY ("lot_id") REFERENCES "lots" ("id");
ALTER TABLE "lots_chunks" ADD FOREIGN KEY ("lot_document_id") REFERENCES "lots_md_documents" ("id");

ALTER TABLE "persons" ADD FOREIGN KEY ("contractor_id") REFERENCES "contractors" ("id");

ALTER TABLE "proposals" ADD FOREIGN KEY ("lot_id") REFERENCES "lots" ("id");
ALTER TABLE "proposals" ADD FOREIGN KEY ("contractor_id") REFERENCES "contractors" ("id");

ALTER TABLE "winners" ADD FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id");

ALTER TABLE "proposal_additional_info" ADD FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id");
ALTER TABLE "proposal_summary_lines" ADD FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id");

ALTER TABLE "position_items" ADD FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id");
ALTER TABLE "position_items" ADD FOREIGN KEY ("catalog_position_id") REFERENCES "catalog_positions" ("id");
ALTER TABLE "position_items" ADD FOREIGN KEY ("unit_id") REFERENCES "units_of_measurement" ("id");

-- Добавление комментариев к полям (уже было в вашем файле, оставлено для полноты)
-- ... (все ваши COMMENT ON ...)

COMMENT ON TABLE "proposals" IS 'Предложения от подрядчиков или базовое предложение от организатора (is_baseline=true).';

COMMENT ON COLUMN "proposals"."contractor_id" IS 'Для базовых предложений ссылается на специального "подрядчика-организатора"';

COMMENT ON COLUMN "proposals"."is_baseline" IS 'True, если это базовое предложение от организатора';

COMMENT ON COLUMN "proposals"."contractor_coordinate" IS 'Координата ячейки подрядчика в Excel (если есть/нужно)';

COMMENT ON COLUMN "proposals"."contractor_width" IS 'Ширина ячейки подрядчика в Excel';

COMMENT ON COLUMN "proposals"."contractor_height" IS 'Высота ячейки подрядчика в Excel';

COMMENT ON COLUMN "winners"."rank" IS 'Место/ранг победителя (1, 2, ...), если применимо';

COMMENT ON COLUMN "winners"."awarded_share" IS 'Доля работ/стоимости (%), присужденная этому победителю, если лот разделен';

COMMENT ON COLUMN "winners"."notes" IS 'Примечания, касающиеся победы этого предложения';

COMMENT ON TABLE "proposal_additional_info" IS 'Дополнительная информация (ключ-значение) для предложения.';

COMMENT ON COLUMN "proposal_additional_info"."proposal_id" IS 'Ссылка на основное или базовое предложение';

COMMENT ON TABLE "proposal_summary_lines" IS 'Итоговые строки (summary) для предложения.';

COMMENT ON COLUMN "proposal_summary_lines"."proposal_id" IS 'Ссылка на основное или базовое предложение';

COMMENT ON COLUMN "proposal_summary_lines"."summary_key" IS 'Ключ из JSON summary (напр., "total_cost_with_vat", "vat")';

COMMENT ON COLUMN "proposal_summary_lines"."job_title" IS 'Наименование итоговой строки';

COMMENT ON TABLE "catalog_positions" IS 'Справочник (каталог) СУТИ стандартизированных позиций (работ, материалов, услуг). Единица измерения для конкретного экземпляра работы определяется ИСКЛЮЧИТЕЛЬНО в таблице position_items (поле unit_in_proposal).';

COMMENT ON COLUMN "catalog_positions"."id" IS 'Уникальный идентификатор СУТИ стандартной позиции';

COMMENT ON COLUMN "catalog_positions"."standard_job_title" IS 'Нормализованное каноническое наименование СУТИ работы/услуги. Уникально. Основной источник для эмбеддинга.';

COMMENT ON COLUMN "catalog_positions"."description" IS 'Общее описание сути работы. Может включать информацию о возможных вариантах измерения или применения. Используется для улучшения эмбеддингов.';

COMMENT ON COLUMN "catalog_positions"."created_at" IS 'Время создания записи';

COMMENT ON COLUMN "catalog_positions"."updated_at" IS 'Время последнего обновления записи';

COMMENT ON TABLE "position_items" IS 'Таблица хранит конкретные строки (позиции) из детализации предложений подрядчиков. Каждая строка включает оригинальные данные из JSON (наименование, ед. изм., количество, стоимости), а также ссылку на соответствующую "суть работы" в справочнике catalog_positions. Уникальность строки в рамках предложения обеспечивается парой (proposal_id, position_key_in_proposal).';

COMMENT ON COLUMN "position_items"."id" IS 'Уникальный идентификатор строки позиции в предложении';

COMMENT ON COLUMN "position_items"."proposal_id" IS 'Ссылка на предложение подрядчика (из таблицы proposals)';

COMMENT ON COLUMN "position_items"."catalog_position_id" IS 'Ссылка на стандартизированную СУТЬ позиции из каталога (таблица catalog_positions)';

COMMENT ON COLUMN "position_items"."position_key_in_proposal" IS 'Ключ позиции из исходного JSON-объекта "positions" (например, "2", "3"). Используется для обеспечения уникальности строки в рамках одного предложения.';

COMMENT ON COLUMN "position_items"."comment_organazier" IS 'Комментарий организатора. Может быть NULL.';

COMMENT ON COLUMN "position_items"."comment_contractor" IS 'Комментарий подрядчика. Может быть NULL.';

COMMENT ON COLUMN "position_items"."item_number_in_proposal" IS 'Порядковый номер позиции, как он указан в JSON (поле "number"). Может быть NULL.';

COMMENT ON COLUMN "position_items"."chapter_number_in_proposal" IS 'Номер главы, как он указан в JSON (поле "chapter_number"). Может быть NULL.';

COMMENT ON COLUMN "position_items"."job_title_in_proposal" IS 'Наименование работы/материала, как оно указано в JSON для этой конкретной строки.';

COMMENT ON COLUMN "position_items"."unit_id" IS 'Ссылка на единицу измерения из справочника units_of_measurement. Может быть NULL, если для позиции не указана ЕИ.';

COMMENT ON COLUMN "position_items"."quantity" IS 'Количество. Может быть NULL (например, для заголовков глав или если не указано).';

COMMENT ON COLUMN "position_items"."suggested_quantity" IS 'Предлагаемое подрядчиком количество. Может быть NULL.';

COMMENT ON COLUMN "position_items"."total_cost_for_organizer_quantity" IS 'Расчетная общая стоимость по ценам подрядчика, но для первоначального количества, указанного организатором. Может быть NULL.';

COMMENT ON COLUMN "position_items"."unit_cost_materials" IS 'Стоимость материалов за единицу';

COMMENT ON COLUMN "position_items"."unit_cost_works" IS 'Стоимость работ за единицу';

COMMENT ON COLUMN "position_items"."unit_cost_indirect_costs" IS 'Накладные расходы (косвенные затраты) за единицу';

COMMENT ON COLUMN "position_items"."unit_cost_total" IS 'Общая стоимость за единицу (может быть суммой предыдущих или отдельным значением)';

COMMENT ON COLUMN "position_items"."total_cost_materials" IS 'Общая стоимость материалов по данной позиции';

COMMENT ON COLUMN "position_items"."total_cost_works" IS 'Общая стоимость работ по данной позиции';

COMMENT ON COLUMN "position_items"."total_cost_indirect_costs" IS 'Общие накладные расходы (косвенные затраты) по данной позиции';

COMMENT ON COLUMN "position_items"."total_cost_total" IS 'Общая итоговая стоимость по данной позиции (может быть суммой или отдельным значением)';

COMMENT ON COLUMN "position_items"."deviation_from_baseline_cost" IS 'Отклонение от расчетной стоимости. Может быть NULL.';

COMMENT ON COLUMN "position_items"."is_chapter" IS 'Флаг, указывающий, является ли эта строка заголовком главы (true) или обычной позицией (false).';

COMMENT ON COLUMN "position_items"."chapter_ref_in_proposal" IS 'Ссылка на номер главы (на поле "item_number_in_proposal" другой строки), к которой относится эта позиция. Используется, если is_chapter = false и позиция вложена в главу. Может быть NULL.';

COMMENT ON COLUMN "position_items"."created_at" IS 'Время создания записи';

COMMENT ON COLUMN "position_items"."updated_at" IS 'Время последнего обновления записи';

COMMENT ON TABLE "units_of_measurement" IS 'Справочник единиц измерения. Заполняется нормализованными значениями из источника.';

COMMENT ON COLUMN "units_of_measurement"."id" IS 'Уникальный идентификатор единицы измерения';

COMMENT ON COLUMN "units_of_measurement"."normalized_name" IS 'Нормализованное наименование из Excel/JSON (например, "шт", "м3", "компл")';

COMMENT ON COLUMN "units_of_measurement"."full_name" IS 'Полное наименование (например, "штука", "метр кубический"). Можно заполнить позже или оставить NULL.';

COMMENT ON COLUMN "units_of_measurement"."description" IS 'Дополнительное описание или комментарий. Может быть NULL.';

COMMENT ON TABLE "lots_md_documents" IS 'Хранит исходные текстовые документы (например, в формате Markdown), связанные с конкретным лотом. Каждый документ из этой таблицы является основой для нарезки на чанки.';

COMMENT ON COLUMN "lots_md_documents"."id" IS 'Уникальный идентификатор записи документа';

COMMENT ON COLUMN "lots_md_documents"."lot_id" IS 'Ссылка на лот, к которому относится этот документ';

COMMENT ON COLUMN "lots_md_documents"."document_name" IS 'Название исходного документа (например, имя файла)';

COMMENT ON COLUMN "lots_md_documents"."full_content" IS 'Полное, неразрезанное содержимое исходного MD-файла. Служит источником правды.';

COMMENT ON TABLE "lots_chunks" IS 'Хранит отдельные текстовые фрагменты (чанки), полученные после разбиения исходного документа из `lots_md_documents`. Эта таблица является основой для RAG-системы.';

COMMENT ON COLUMN "lots_chunks"."id" IS 'Уникальный идентификатор чанка';

COMMENT ON COLUMN "lots_chunks"."lot_document_id" IS 'Ссылка на родительский документ, из которого был нарезан этот чанк';

COMMENT ON COLUMN "lots_chunks"."chunk_index" IS 'Порядковый номер чанка в рамках одного документа (начиная с 0). Важен для восстановления последовательности текста.';

COMMENT ON COLUMN "lots_chunks"."chunk_text" IS 'Непосредственно текстовое содержимое чанка.';

COMMENT ON COLUMN "lots_chunks"."embedding" IS 'Векторное представление (эмбеддинг) поля chunk_text. Используется для семантического поиска. Размерность зависит от используемой ML-модели.';

COMMENT ON COLUMN "lots_chunks"."metadata" IS 'Дополнительные структурированные метаданные, извлеченные из чанка (например, заголовок раздела, к которому он относится).';
