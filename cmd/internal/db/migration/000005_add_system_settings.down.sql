-- =====================================================================================
-- Rollback Migration 000005: Drop system_settings table
-- =====================================================================================

-- 1. Удаляем триггер и функцию
DROP TRIGGER IF EXISTS system_settings_set_updated_at ON system_settings;
DROP FUNCTION IF EXISTS trg_system_settings_updated_at();

-- 2. Удаляем таблицу (каскадно удалит constraint)
DROP TABLE IF EXISTS system_settings;
