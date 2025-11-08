-- catalog_position.sql
-- (Версия 3, финальная, с `kind` и `VIEW`)
-- Запросы для работы с "золотым" справочником catalog_positions.

-- name: CreateCatalogPosition :one
-- Создает новую запись в каталоге стандартных позиций.
-- ВАЖНО (v4): Теперь ОБЯЗАТЕЛЬНО принимает `kind`.
INSERT INTO catalog_positions (
    standard_job_title,
    description,
    kind
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetCatalogPositionByID :one
-- Получает стандартную позицию по её ID.
SELECT * FROM catalog_positions
WHERE id = $1;

-- name: GetCatalogPositionByStandardJobTitle :one
-- Получает стандартную позицию по её уникальному standard_job_title.
SELECT * FROM catalog_positions
WHERE standard_job_title = $1;

-- name: ListCatalogPositions :many
-- (Улучшено) Получает список всех стандартных позиций (для админки).
-- Не выбирает "тяжелое" поле `embedding`.
SELECT id, standard_job_title, description, kind, created_at, updated_at
FROM catalog_positions
ORDER BY standard_job_title
LIMIT $1
OFFSET $2;

-- name: ListCatalogPositionsForEmbedding :many
-- Получает "очередь" для Python-воркера.
-- Выбирает только `kind = 'POSITION'`, у которых еще нет эмбеддинга.
SELECT id, standard_job_title, description FROM catalog_positions
WHERE 
    embedding IS NULL 
    AND kind = 'POSITION'
ORDER BY id -- Для последовательной обработки
LIMIT $1 -- Обрабатываем пачками
OFFSET $2;

-- name: UpdateCatalogPositionDetails :one
-- Обновляет текстовые детали И/ИЛИ `kind` существующей позиции.
-- При этом ОБНУЛЯЕТ существующий эмбеддинг, так как он становится неактуальным.
UPDATE catalog_positions
SET
    standard_job_title = COALESCE(sqlc.narg(standard_job_title), standard_job_title),
    description = COALESCE(sqlc.narg(description), description),
    kind = COALESCE(sqlc.narg(kind), kind),
    embedding = NULL, -- Сбрасываем эмбеддинг, т.к. текст/суть изменились
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: UpdateCatalogPositionEmbedding :one
-- Обновляет поле embedding для существующей стандартной позиции.
-- Вызывается Python-воркером после генерации эмбеддинга.
UPDATE catalog_positions
SET
    embedding = sqlc.arg(embedding),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCatalogPosition :exec
-- Удаляет стандартную позицию по ID.
-- Вызывается Go-сервером при слиянии дубликатов.
DELETE FROM catalog_positions
WHERE id = $1;

-- name: SearchCatalogPositions :many
-- (Улучшено) Ищет стандартные позиции по частичному совпадению.
-- ВАЖНО (v4): Использует VIEW `catalog_positions_clean`
-- для поиска только среди "чистых" позиций (`kind = 'POSITION'`).
SELECT id, standard_job_title, description, kind, created_at, updated_at
FROM catalog_positions_clean
WHERE standard_job_title ILIKE '%' || sqlc.arg(search_term)::text || '%'
ORDER BY standard_job_title
LIMIT sqlc.arg(page_limit)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: ListCatalogPositionsToReview :many
-- Получает "очередь" для админ-панели (ручная модерация).
-- Ищет записи, которые Go-сервер не смог классифицировать (`kind = 'TO_REVIEW'`).
SELECT * FROM catalog_positions
WHERE kind = 'TO_REVIEW'
ORDER BY created_at DESC
LIMIT $1
OFFSET $2;