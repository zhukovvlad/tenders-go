-- =====================================================================================
-- ЕДИНАЯ МИГРАЦИЯ: init_schema (Consolidated v1-v7)
-- Описание:
-- Полная структура базы данных системы анализа тендеров.
-- Включает:
-- 1. RAG-инфраструктуру (векторы, чанки, кэш матчинга).
-- 2. Иерархические справочники и тендерные данные.
-- 3. Аутентификацию (users, sessions).
-- 4. Статусную модель и версионирование каталога.
-- =====================================================================================

-- 1. АКТИВАЦИЯ РАСШИРЕНИЙ
CREATE EXTENSION IF NOT EXISTS vector;

-- =====================================================================================
-- РАЗДЕЛ 1: СПРАВОЧНИКИ И ЕДИНИЦЫ ИЗМЕРЕНИЯ
-- =====================================================================================

-- Единицы измерения (создаем первыми, так как каталог зависит от них)
CREATE TABLE "units_of_measurement" (
  "id" BIGSERIAL PRIMARY KEY,
  "normalized_name" varchar(50) UNIQUE NOT NULL,
  "full_name" text,
  "description" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "tender_types" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now())
);

CREATE TABLE "tender_chapters" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "tender_type_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("tender_type_id") REFERENCES "tender_types" ("id") ON DELETE CASCADE
);

CREATE TABLE "tender_categories" (
  "id" BIGSERIAL PRIMARY KEY,
  "title" varchar UNIQUE NOT NULL,
  "tender_chapter_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("tender_chapter_id") REFERENCES "tender_chapters" ("id") ON DELETE CASCADE
);

-- =====================================================================================
-- РАЗДЕЛ 2: КАТАЛОГ РАБОТ (RAG CORE)
-- =====================================================================================

CREATE TABLE "catalog_positions" (
  "id" BIGSERIAL PRIMARY KEY,
  "standard_job_title" text NOT NULL,
  "description" text,
  "embedding" vector(768),
  
  -- Классификация (Position vs Header vs Trash)
  "kind" TEXT NOT NULL DEFAULT 'TO_REVIEW',
  
  -- Жизненный цикл (Active vs Pending vs Deprecated)
  "status" VARCHAR(50) NOT NULL DEFAULT 'na',
  
  -- Связь с единицами измерения (часть уникальности)
  "unit_id" bigint,

  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),

  -- Ограничения целостности
  CONSTRAINT "ck_catalog_positions_kind" 
    CHECK ("kind" IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW')),
  CONSTRAINT "chk_catalog_positions_status"
    CHECK ("status" IN ('pending_indexing', 'active', 'deprecated', 'archived', 'na')),
  CONSTRAINT "catalog_positions_unit_id_fkey" 
    FOREIGN KEY ("unit_id") REFERENCES "units_of_measurement" ("id") ON DELETE SET NULL
);

-- Уникальный индекс: Название + Ед.Изм (COALESCE для обработки NULL unit_id)
CREATE UNIQUE INDEX "uq_catalog_positions_title_unit" 
ON "catalog_positions" ("standard_job_title", COALESCE("unit_id", -1));

-- Частичный HNSW-индекс ТОЛЬКО для реальных позиций (ускорение RAG)
CREATE INDEX "idx_cp_kind_pos_hnsw"
ON "catalog_positions" USING HNSW (embedding vector_cosine_ops)
WHERE "kind" = 'POSITION';

-- Индексы для админки и воркеров
CREATE INDEX "idx_catalog_positions_kind" ON "catalog_positions" ("kind");
CREATE INDEX "idx_cp_kind_review" ON "catalog_positions" (id) WHERE "kind" = 'TO_REVIEW';
CREATE INDEX "idx_cp_status_pending" ON "catalog_positions" (id) WHERE status = 'pending_indexing';
CREATE INDEX "idx_cp_status" ON "catalog_positions" (status);


-- VIEW для "чистого" поиска
CREATE OR REPLACE VIEW "catalog_positions_clean" AS
SELECT * FROM "catalog_positions" WHERE "kind" = 'POSITION';


-- Таблица кэширования матчинга (RAG Cache)
CREATE TABLE "matching_cache" (
  "job_title_hash" TEXT NOT NULL,
  "norm_version" SMALLINT NOT NULL DEFAULT 1,
  "job_title_text" TEXT,
  "catalog_position_id" BIGINT NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  "expires_at" TIMESTAMPTZ, -- TTL

  PRIMARY KEY ("job_title_hash", "norm_version"),
  FOREIGN KEY ("catalog_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE
);
CREATE INDEX "idx_matching_cache_catalog_id" ON "matching_cache" ("catalog_position_id");
CREATE INDEX "idx_matching_cache_expires_at" ON "matching_cache" ("expires_at");


-- Таблица предложений по слиянию (Human-in-the-Loop)
CREATE TABLE "suggested_merges" (
  "id" BIGSERIAL PRIMARY KEY,
  "main_position_id" BIGINT NOT NULL,
  "duplicate_position_id" BIGINT NOT NULL,
  "similarity_score" REAL NOT NULL,
  "status" TEXT NOT NULL DEFAULT 'PENDING',
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  "decided_at" TIMESTAMPTZ,
  "decided_by" TEXT,

  CONSTRAINT "ck_suggested_merges_status" CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED')),
  CONSTRAINT "uq_suggestion" UNIQUE ("main_position_id", "duplicate_position_id"),
  FOREIGN KEY ("main_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE,
  FOREIGN KEY ("duplicate_position_id") REFERENCES "catalog_positions"("id") ON DELETE CASCADE
);
CREATE INDEX "idx_suggested_merges_status_created" 
ON "suggested_merges" ("status", "created_at" DESC) WHERE "status" = 'PENDING';


-- =====================================================================================
-- РАЗДЕЛ 3: ОСНОВНЫЕ СУЩНОСТИ (ТЕНДЕРЫ, ЗАКАЗЧИКИ, ЛОТЫ)
-- =====================================================================================

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

CREATE TABLE "tenders" (
  "id" BIGSERIAL PRIMARY KEY,
  "etp_id" varchar UNIQUE NOT NULL,
  "title" varchar NOT NULL,
  "category_id" bigint,
  "object_id" bigint NOT NULL,
  "executor_id" bigint NOT NULL,
  "data_prepared_on_date" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),

  FOREIGN KEY ("category_id") REFERENCES "tender_categories" ("id") ON DELETE SET NULL,
  FOREIGN KEY ("object_id") REFERENCES "objects" ("id"),
  FOREIGN KEY ("executor_id") REFERENCES "executors" ("id")
);

-- Таблица сырых данных (Source of Truth)
CREATE TABLE "tender_raw_data" (
  "tender_id" BIGINT PRIMARY KEY,
  "raw_data" JSONB NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  "updated_at" TIMESTAMPTZ NOT NULL DEFAULT (now()),
  FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id") ON DELETE CASCADE
);

CREATE TABLE "lots" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_key" varchar NOT NULL,
  "lot_title" varchar NOT NULL,
  "lot_key_parameters" jsonb,
  "tender_id" bigint NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  
  -- БЕЗ CASCADE: нельзя удалить тендер, пока есть лоты (как в оригинале)
  FOREIGN KEY ("tender_id") REFERENCES "tenders" ("id")
);
CREATE UNIQUE INDEX ON "lots" ("tender_id", "lot_key");
CREATE INDEX "idx_gin_lots_key_parameters" ON lots USING GIN (lot_key_parameters);

-- Документация к лотам (RAG Documents)
CREATE TABLE "lots_md_documents" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_id" bigint NOT NULL,
  "document_name" varchar(255) NOT NULL,
  "full_content" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id") ON DELETE CASCADE
);

CREATE TABLE "lots_chunks" (
  "id" BIGSERIAL PRIMARY KEY,
  "lot_document_id" bigint NOT NULL,
  "chunk_index" int NOT NULL,
  "chunk_text" text NOT NULL,
  "embedding" vector(768) NOT NULL,
  "metadata" jsonb,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("lot_document_id") REFERENCES "lots_md_documents" ("id") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "uq_chunk_index_per_document" ON "lots_chunks" ("lot_document_id", "chunk_index");
CREATE INDEX "lots_chunks_embedding_idx" ON lots_chunks USING HNSW (embedding vector_cosine_ops);


-- =====================================================================================
-- РАЗДЕЛ 4: ПОДРЯДЧИКИ И ПРЕДЛОЖЕНИЯ
-- =====================================================================================

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
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("contractor_id") REFERENCES "contractors" ("id")
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
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  
  FOREIGN KEY ("lot_id") REFERENCES "lots" ("id") ON DELETE CASCADE,
  FOREIGN KEY ("contractor_id") REFERENCES "contractors" ("id")
);
CREATE UNIQUE INDEX "uq_proposals_lot_contractor" ON "proposals" ("lot_id", "contractor_id");

CREATE TABLE "winners" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint UNIQUE NOT NULL,
  "rank" int,
  "awarded_share" numeric(5,2),
  "notes" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id") ON DELETE CASCADE
);

CREATE TABLE "proposal_additional_info" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint NOT NULL,
  "info_key" text NOT NULL,
  "info_value" text,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "uq_proposal_additional_info_key" ON "proposal_additional_info" ("proposal_id", "info_key");

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
  "updated_at" timestamptz NOT NULL DEFAULT (now()),
  FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id") ON DELETE CASCADE
);
CREATE UNIQUE INDEX "uq_proposal_summary_lines_key" ON "proposal_summary_lines" ("proposal_id", "summary_key");


-- Детализация предложения (Строки сметы)
CREATE TABLE "position_items" (
  "id" BIGSERIAL PRIMARY KEY,
  "proposal_id" bigint NOT NULL,
  
  -- Ссылка на каталог может быть NULL (если еще не обработано)
  "catalog_position_id" bigint,
  
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
  "updated_at" timestamptz NOT NULL DEFAULT (now()),

  FOREIGN KEY ("proposal_id") REFERENCES "proposals" ("id") ON DELETE CASCADE,
  FOREIGN KEY ("catalog_position_id") REFERENCES "catalog_positions" ("id"),
  FOREIGN KEY ("unit_id") REFERENCES "units_of_measurement" ("id")
);
CREATE UNIQUE INDEX "uq_position_items_proposal_id_key" ON "position_items" ("proposal_id", "position_key_in_proposal");
CREATE INDEX "idx_position_items_proposal_id" ON "position_items" ("proposal_id");
CREATE INDEX "idx_position_items_catalog_id" ON "position_items" ("catalog_position_id");
CREATE INDEX "idx_position_items_unit_id" ON "position_items" ("unit_id");


-- =====================================================================================
-- РАЗДЕЛ 5: АВТОРИЗАЦИЯ
-- =====================================================================================

CREATE TABLE "users" (
  "id" BIGSERIAL PRIMARY KEY,
  "email" varchar(255) NOT NULL,
  "password_hash" text NOT NULL,
  "role" varchar(50) NOT NULL DEFAULT 'operator',
  "is_active" boolean NOT NULL DEFAULT true,
  "last_login_at" timestamptz,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "updated_at" timestamptz NOT NULL DEFAULT (now()),

  CONSTRAINT "uq_users_email" UNIQUE ("email"),
  CONSTRAINT "chk_users_email_normalized" CHECK ("email" = LOWER(BTRIM("email"))),
  CONSTRAINT "chk_users_role" CHECK ("role" IN ('admin', 'operator', 'viewer'))
);

CREATE TABLE "user_sessions" (
  "id" BIGSERIAL PRIMARY KEY,
  "user_id" BIGINT NOT NULL,
  "refresh_token_hash" char(64) NOT NULL,
  "user_agent" text,
  "ip_address" inet,
  "created_at" timestamptz NOT NULL DEFAULT (now()),
  "expires_at" timestamptz NOT NULL,
  "revoked_at" timestamptz,

  CONSTRAINT "fk_user_sessions_user" FOREIGN KEY ("user_id") REFERENCES "users"("id") ON DELETE CASCADE,
  CONSTRAINT "uq_user_sessions_refresh_hash" UNIQUE ("refresh_token_hash"),
  CONSTRAINT "chk_user_sessions_expires_at" CHECK ("expires_at" > "created_at"),
  CONSTRAINT "chk_user_sessions_refresh_hash_hex" CHECK ("refresh_token_hash" ~ '^[0-9a-f]{64}$')
);

-- Индексы для user_sessions
CREATE INDEX "idx_user_sessions_user_id" ON "user_sessions" ("user_id");
CREATE INDEX "idx_user_sessions_expires_at" ON "user_sessions" ("expires_at");
CREATE INDEX "idx_user_sessions_user_active" ON "user_sessions" ("user_id", "expires_at") WHERE "revoked_at" IS NULL;
