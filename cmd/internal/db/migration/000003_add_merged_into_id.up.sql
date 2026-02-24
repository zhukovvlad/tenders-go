-- 1. Добавляем колонку merged_into_id
-- Она NULLABLE, так как у активных (мастер) позиций она пустая.
ALTER TABLE catalog_positions 
ADD COLUMN merged_into_id BIGINT NULL;

-- 2. Создаем внешний ключ (Foreign Key)
-- Ссылаемся на id в этой же таблице.
-- ON DELETE RESTRICT: Запрещаем удалять мастер-позицию, если в нее влиты другие.
-- Это критически важно для безопасности данных.
ALTER TABLE catalog_positions
ADD CONSTRAINT fk_catalog_positions_merged_into
FOREIGN KEY (merged_into_id) 
REFERENCES catalog_positions(id)
ON DELETE RESTRICT;

-- 3. Добавляем индекс
-- Чтобы мгновенно находить "все старые версии" для конкретного мастера.
-- Например: SELECT * FROM catalog_positions WHERE merged_into_id = 100;
CREATE INDEX idx_catalog_positions_merged_into 
ON catalog_positions(merged_into_id);

-- 4. Добавляем проверку (Check Constraint)
-- Защита от дурака: запрещаем позиции ссылаться на саму себя.
ALTER TABLE catalog_positions
ADD CONSTRAINT chk_not_self_merge 
CHECK (id <> merged_into_id);