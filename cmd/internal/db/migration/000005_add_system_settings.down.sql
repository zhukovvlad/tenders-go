-- =====================================================================================
-- Rollback Migration 000005: Drop system_settings table
-- =====================================================================================

-- 1. Удаляем таблицу (CASCADE удалит триггер и constraint автоматически)
DROP TABLE IF EXISTS system_settings CASCADE;

-- 2. Удаляем функцию триггера (не удаляется каскадом с таблицей)
DROP FUNCTION IF EXISTS trg_system_settings_updated_at();
