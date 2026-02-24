-- Удаление колонки автоматически удалит и внешний ключ, и индекс, и constraint.
-- ВНИМАНИЕ: откат этой миграции приведёт к безвозвратной потере истории объединений,
-- так как значения catalog_positions.merged_into_id будут удалены.
-- Запускайте этот down-скрипт только если:
--   (1) объединения позиций ещё не выполнялись, или
--   (2) вы сознательно отказываетесь от сохранения истории объединений.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM catalog_positions WHERE merged_into_id IS NOT NULL) THEN
        RAISE WARNING 'catalog_positions.merged_into_id contains non-NULL values. Merge history will be permanently lost.';
    END IF;
END;
$$;

ALTER TABLE catalog_positions 
DROP COLUMN IF EXISTS merged_into_id;