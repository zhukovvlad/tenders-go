-- 1. Обновляем CHECK constraint для suggested_merges
-- Добавляем статус EXECUTED для выполненных слияний (отличается от APPROVED).
ALTER TABLE suggested_merges
DROP CONSTRAINT "ck_suggested_merges_status";

ALTER TABLE suggested_merges
ADD CONSTRAINT "ck_suggested_merges_status" 
CHECK ("status" IN ('PENDING', 'APPROVED', 'REJECTED', 'EXECUTED'));

-- 2. Добавляем колонки для отслеживания исполнения (отдельно от approval)
ALTER TABLE suggested_merges
ADD COLUMN executed_at TIMESTAMPTZ,
ADD COLUMN executed_by TEXT;
