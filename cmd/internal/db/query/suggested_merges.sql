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
-- Пагинация применяется на уровне групп (main_position_id), а не строк,
-- чтобы все дубликаты для группы всегда возвращались вместе.
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
    AND sm.main_position_id IN (
        SELECT sub.main_position_id
        FROM suggested_merges sub
        WHERE sub.status = 'PENDING'
        GROUP BY sub.main_position_id
        ORDER BY MAX(sub.similarity_score) DESC, sub.main_position_id ASC
        LIMIT $1
        OFFSET $2
    )
ORDER BY 
    sm.similarity_score DESC, sm.main_position_id ASC, sm.id ASC;

-- name: CountPendingMerges :one
-- Возвращает общее количество PENDING merge-предложений (для пагинации).
SELECT COUNT(*) FROM suggested_merges WHERE status = 'PENDING';

-- name: CountPendingMergeGroups :one
-- Возвращает количество уникальных мастер-позиций среди PENDING merge-предложений (для пагинации).
SELECT COUNT(DISTINCT main_position_id) FROM suggested_merges WHERE status = 'PENDING';

-- name: UpdateMergeStatus :one
-- (Для Go-сервера / Админки) Обновляет статус задачи
-- (APPROVED / REJECTED).
UPDATE suggested_merges
SET 
    status = $1,
    resolved_at = NOW(),
    resolved_by = $2
WHERE 
    id = $3
RETURNING *;

-- name: RejectPendingMerge :one
-- Атомарно переводит предложение из PENDING в REJECTED (защита от race condition).
-- Возвращает строку ТОЛЬКО если текущий статус = PENDING.
-- Если строка не найдена или статус не PENDING — возвращает sql.ErrNoRows.
UPDATE suggested_merges
SET 
    status = 'REJECTED',
    resolved_at = NOW(),
    resolved_by = $1
WHERE 
    id = $2
    AND status = 'PENDING'
RETURNING *;

-- name: GetSuggestedMergeByID :one
-- Получает предложение о слиянии по ID (для валидации перед исполнением).
SELECT * FROM suggested_merges
WHERE id = $1;

-- name: ExecuteMerge :one
-- Атомарно переводит предложение из PENDING или APPROVED в EXECUTED (one-click merge).
-- Возвращает строку ТОЛЬКО если текущий статус позволяет исполнение (защита от race condition).
-- Используется внутри транзакции:
--   • Default Merge (Сценарий 1): в паре с MergeCatalogPosition
--   • Merge-to-New (Сценарий 2): в паре с CreateSimpleCatalogPosition + SetPositionMerged
UPDATE suggested_merges
SET 
    status = 'EXECUTED',
    resolved_at = NOW(),
    resolved_by = $1
WHERE 
    id = $2
    AND status IN ('PENDING', 'APPROVED')
RETURNING *;

-- name: ExecuteMergeBatch :many
-- Атомарно переводит несколько предложений из PENDING/APPROVED в EXECUTED (batch merge).
-- Возвращает ВСЕ строки, успешно обновлённые. Если len(returned) < len(ids) — часть
-- merge'ей не прошла (неверный статус или не найдены).
-- Используется внутри транзакции execute-batch.
UPDATE suggested_merges
SET
    status = 'EXECUTED',
    resolved_at = NOW(),
    resolved_by = @resolved_by
WHERE
    id = ANY(@ids::bigint[])
    AND status IN ('PENDING', 'APPROVED')
RETURNING *;

-- name: InvalidateRelatedActionableMerges :exec
-- Инвалидирует (REJECTED) все незавершённые (PENDING/APPROVED) заявки, где участвует
-- хотя бы одна из deprecated-позиций (после слияния). Решает проблему "мёртвых душ":
-- когда позиция B влита в A, другие заявки с участием B
-- зависают навсегда и вызывают ошибки при попытке исполнения.
UPDATE suggested_merges
SET
    status = 'REJECTED',
    resolved_at = NOW(),
    resolved_by = 'system'
WHERE
    status IN ('PENDING', 'APPROVED')
    AND (
        main_position_id = ANY(@position_ids::bigint[])
        OR duplicate_position_id = ANY(@position_ids::bigint[])
    );

-- name: DeleteOutdatedPendingMerges :exec
-- Очищает PENDING-заявки на слияние, которые больше не проходят по новому порогу расстояния.
-- Формула: similarity_score = 1.0 - distance.
-- Если порог стал строже (distance уменьшился), то (1.0 - distance) растёт,
-- и все заявки с similarity_score ниже нового порога удаляются.
DELETE FROM suggested_merges
WHERE status = 'PENDING'
  AND similarity_score < (1.0 - sqlc.arg(distance_threshold)::float8);