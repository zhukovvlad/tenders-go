-- =====================================================================================
-- Migration 000007: Add Variants Grouping and JSONB Parameters
-- =====================================================================================

-- 1. Добавляем колонки в каталог
ALTER TABLE catalog_positions
ADD COLUMN parent_id BIGINT NULL,
ADD COLUMN parameters JSONB NULL; -- Оставляем NULL по умолчанию для очереди воркера

-- 2. Внешний ключ для связи Родитель -> Ребенок
ALTER TABLE catalog_positions
ADD CONSTRAINT fk_catalog_positions_parent
FOREIGN KEY (parent_id) 
REFERENCES catalog_positions(id)
ON DELETE RESTRICT;

-- 3. Защита от прямой самоссылки
ALTER TABLE catalog_positions
ADD CONSTRAINT chk_not_self_parent 
CHECK (id <> parent_id);

-- 4. Индексы
-- B-Tree для быстрого поиска всех детей конкретного родителя
CREATE INDEX idx_catalog_positions_parent_id 
ON catalog_positions(parent_id);

-- GIN-индекс для аналитики и фильтрации по внутренностям JSON
-- Позволяет мгновенно искать: WHERE parameters @> '{"material": "ПВХ"}'
CREATE INDEX idx_catalog_positions_parameters_gin 
ON catalog_positions USING GIN (parameters);

-- 5. Расширяем статусную модель очереди слияний
-- Удаляем старый constraint (из миграции 000004)
ALTER TABLE suggested_merges
DROP CONSTRAINT ck_suggested_merges_status;

-- Добавляем новый статус 'GROUPED'
ALTER TABLE suggested_merges
ADD CONSTRAINT ck_suggested_merges_status
CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'EXECUTED', 'GROUPED'));
