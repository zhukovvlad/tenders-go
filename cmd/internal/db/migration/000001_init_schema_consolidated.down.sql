-- =====================================================================================
-- ЕДИНАЯ МИГРАЦИЯ: init_schema (Consolidated v1-v7) — ОТКАТ
-- =====================================================================================

-- Удаляем VIEW
DROP VIEW IF EXISTS "catalog_positions_clean";

-- Удаляем таблицы в обратном порядке зависимостей

-- Авторизация
DROP TABLE IF EXISTS "user_sessions";
DROP TABLE IF EXISTS "users";

-- Предложения и детализация
DROP TABLE IF EXISTS "position_items";
DROP TABLE IF EXISTS "proposal_summary_lines";
DROP TABLE IF EXISTS "proposal_additional_info";
DROP TABLE IF EXISTS "winners";
DROP TABLE IF EXISTS "proposals";
DROP TABLE IF EXISTS "persons";
DROP TABLE IF EXISTS "contractors";

-- Лоты и RAG-документы
DROP TABLE IF EXISTS "lots_chunks";
DROP TABLE IF EXISTS "lots_md_documents";
DROP TABLE IF EXISTS "lots";
DROP TABLE IF EXISTS "tender_raw_data";
DROP TABLE IF EXISTS "tenders";

-- Основные сущности
DROP TABLE IF EXISTS "executors";
DROP TABLE IF EXISTS "objects";

-- RAG-инфраструктура каталога
DROP TABLE IF EXISTS "suggested_merges";
DROP TABLE IF EXISTS "matching_cache";
DROP TABLE IF EXISTS "catalog_positions";

-- Справочники
DROP TABLE IF EXISTS "tender_categories";
DROP TABLE IF EXISTS "tender_chapters";
DROP TABLE IF EXISTS "tender_types";
DROP TABLE IF EXISTS "units_of_measurement";

-- Расширения (опционально — обычно не удаляют)
-- DROP EXTENSION IF EXISTS vector;
