-- 1. Откатываем EXECUTED обратно в APPROVED (чтобы constraint не ломался)
-- WARNING:
--   Данная down-миграция конвертирует все записи со статусом EXECUTED обратно в APPROVED
--   и удаляет метаданные исполнения (executed_at, executed_by).
--   Это приводит к потере информации о том, был ли merge фактически выполнен.
--   Откатывать эту миграцию имеет смысл только вместе с откатом связанных
--   изменений catalog_positions (down-миграция 000003), чтобы не получить
--   несогласованное состояние данных (catalog_positions уже изменены, а
--   suggested_merges выглядит как только APPROVED).
--   Если откат выполняется без отката 000003, это считается осознанной
--   потерей данных о факте исполнения merge.
UPDATE suggested_merges SET status = 'APPROVED' WHERE status = 'EXECUTED';

-- 2. Удаляем колонки исполнения
ALTER TABLE suggested_merges
DROP COLUMN IF EXISTS executed_at,
DROP COLUMN IF EXISTS executed_by;

-- 3. Возвращаем старый CHECK constraint
ALTER TABLE suggested_merges
DROP CONSTRAINT "ck_suggested_merges_status";

ALTER TABLE suggested_merges
ADD CONSTRAINT "ck_suggested_merges_status" 
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED'));
