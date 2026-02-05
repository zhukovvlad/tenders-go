-- =====================================================================================
-- ФАЙЛ ОТКАТА ПЕРВОНАЧАЛЬНОЙ МИГРАЦИИ
-- Версия: 1 (Down)
-- Описание:
-- Этот скрипт полностью отменяет действия, выполненные в миграции
-- `000001_init_schema.up.sql`. Он удаляет все созданные таблицы и отключает
-- расширение vector.
--
-- Порядок действий важен: сначала удаляются таблицы (CASCADE автоматически
-- удалит связанные с ними внешние ключи, индексы и последовательности),
-- а затем отключается расширение.
-- =====================================================================================

-- --- ШАГ 1: Удаление всех таблиц ---
-- Используем `DROP TABLE IF EXISTS ... CASCADE` для безопасного и полного удаления.
-- CASCADE автоматически удалит все зависимые объекты, такие как внешние ключи и индексы.
-- Порядок удаления таблиц здесь не так важен благодаря CASCADE, но для наглядности
-- можно следовать обратному порядку создания.

DROP TABLE IF EXISTS "lots_chunks" CASCADE;
DROP TABLE IF EXISTS "lots_md_documents" CASCADE;
DROP TABLE IF EXISTS "winners" CASCADE;
DROP TABLE IF EXISTS "proposal_additional_info" CASCADE;
DROP TABLE IF EXISTS "proposal_summary_lines" CASCADE;
DROP TABLE IF EXISTS "position_items" CASCADE;
DROP TABLE IF EXISTS "proposals" CASCADE;
DROP TABLE IF EXISTS "persons" CASCADE;
DROP TABLE IF EXISTS "contractors" CASCADE;
DROP TABLE IF EXISTS "lots" CASCADE;
DROP TABLE IF EXISTS "tenders" CASCADE;
DROP TABLE IF EXISTS "tender_categories" CASCADE;
DROP TABLE IF EXISTS "tender_chapters" CASCADE;
DROP TABLE IF EXISTS "tender_types" CASCADE;
DROP TABLE IF EXISTS "objects" CASCADE;
DROP TABLE IF EXISTS "executors" CASCADE;
DROP TABLE IF EXISTS "catalog_positions" CASCADE;
DROP TABLE IF EXISTS "units_of_measurement" CASCADE;


-- --- ШАГ 2: Отключение расширения ---
-- Эта команда должна быть последней, так как она не выполнится,
-- если в базе данных все еще существуют объекты, использующие тип `vector`.
DROP EXTENSION IF EXISTS vector;