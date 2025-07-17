-- ФАЙЛ МИГРАЦИИ: 000003_add_raw_data_table.down.sql
-- Описание:
-- Откатывает миграцию, удаляя таблицу "tender_raw_data".

DROP TABLE IF EXISTS "tender_raw_data";