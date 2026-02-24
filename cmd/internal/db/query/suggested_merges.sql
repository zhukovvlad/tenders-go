-- suggested_merges.sql
-- Запросы для работы с очередью "слияния дубликатов".

-- name: UpsertSuggestedMerge :exec
-- (Для RAG-воркера) Создает или обновляет "задачу" для оператора.
--
INSERT INTO suggested_merges (
    main_position_id,
    duplicate_position_id,
    similarity_score
) VALUES (
    $1, $2, $3
)
ON CONFLICT (main_position_id, duplicate_position_id) 
DO UPDATE 
SET
    similarity_score = EXCLUDED.similarity_score, -- Всегда обновляем скор
    -- Статус сбрасываем в PENDING, только если он не был окончательно решен
    status = CASE WHEN suggested_merges.status IN ('APPROVED', 'REJECTED', 'EXECUTED') THEN suggested_merges.status ELSE 'PENDING' END,
    updated_at = NOW(); -- Обновляем время изменения, но не создания

-- name: ListPendingMerges :many
-- (Для Go-сервера / Админки) Показывает оператору "очередь"
-- того, что нужно смерджить.
SELECT 
    sqlc.embed(sm),
    sqlc.embed(main_pos),
    sqlc.embed(dup_pos)
FROM 
    suggested_merges sm
JOIN 
    catalog_positions main_pos ON sm.main_position_id = main_pos.id
JOIN 
    catalog_positions dup_pos ON sm.duplicate_position_id = dup_pos.id
WHERE 
    sm.status = 'PENDING'
ORDER BY 
    sm.similarity_score DESC
LIMIT $1
OFFSET $2;

-- name: UpdateMergeStatus :one
-- (Для Go-сервера / Админки) Обновляет статус задачи
-- (APPROVED / REJECTED).
UPDATE suggested_merges
SET 
    status = $1,
    decided_at = NOW(),
    decided_by = $2
WHERE 
    id = $3
RETURNING *;

-- name: GetSuggestedMergeByID :one
-- Получает предложение о слиянии по ID (для валидации перед исполнением).
SELECT * FROM suggested_merges
WHERE id = $1;

-- name: ExecuteApprovedMerge :one
-- Атомарно переводит предложение из APPROVED в EXECUTED.
-- Возвращает строку ТОЛЬКО если текущий статус = 'APPROVED' (защита от race condition).
-- Используется внутри транзакции вместе с MergeCatalogPosition.
UPDATE suggested_merges
SET 
    status = 'EXECUTED',
    executed_at = NOW(),
    executed_by = $1
WHERE 
    id = $2
    AND status = 'APPROVED'
RETURNING *;