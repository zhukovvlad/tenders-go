-- 1. Откатываем EXECUTED обратно в APPROVED (чтобы constraint не ломался)
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
