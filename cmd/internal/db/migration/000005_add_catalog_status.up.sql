-- 000005_add_catalog_status.up.sql
ALTER TABLE catalog_positions
-- (ИЗМЕНЕНИЕ 1) 'na' (Not Applicable) - это наш новый 'нейтральный' DEFAULT
ADD COLUMN status VARCHAR(50) NOT NULL DEFAULT 'na';

ALTER TABLE catalog_positions
ADD CONSTRAINT chk_catalog_positions_status
CHECK (status IN (
    'pending_indexing', -- Ожидает индексации (только для kind=POSITION)
    'active',           -- В индексе
    'deprecated',       -- Устарела
    'archived',         -- В архиве
    'na'                -- Неприменимо (для HEADER, TRASH и т.д.)
));

COMMENT ON COLUMN catalog_positions.status
IS 'Жизненный цикл позиции: pending_indexing, active, deprecated, archived, na (not applicable)';

-- Индекс для очереди RAG-воркера (все еще нужен)
CREATE INDEX IF NOT EXISTS idx_cp_status_pending
ON catalog_positions (id)
WHERE status = 'pending_indexing';

-- Индекс для админки (все еще нужен)
CREATE INDEX IF NOT EXISTS idx_cp_status
ON catalog_positions (status);