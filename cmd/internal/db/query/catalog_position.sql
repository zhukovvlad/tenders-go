-- catalog_position.sql
-- (Версия 3, финальная, с `kind` и `VIEW`)
-- Запросы для работы с "золотым" справочником catalog_positions.

-- name: CreateCatalogPosition :one
-- ВАЖНО (v5): Добавлен unit_id
INSERT INTO catalog_positions (
    standard_job_title,
    description,
    kind,
    status,
    unit_id
) VALUES (
    $1, $2, $3, $4, $5
)
RETURNING *;

-- name: GetCatalogPositionByID :one
-- Получает стандартную позицию по её ID.
SELECT * FROM catalog_positions
WHERE id = $1;

-- name: GetCatalogPositionByTitleAndUnit :one
-- ВАЖНО: Ищем по паре (Title + Unit).
-- Хитрая проверка (unit_id IS NULL AND $2 IS NULL) нужна для корректной работы с NULL в Go.
SELECT * FROM catalog_positions
WHERE standard_job_title = $1
  AND (unit_id = sqlc.narg('unit_id') OR (unit_id IS NULL AND sqlc.narg('unit_id') IS NULL));

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
    unit_id = COALESCE(sqlc.narg(unit_id), unit_id),
    status = 'pending_indexing', -- Обязательно сбрасываем для воркера
    embedding = NULL,           -- Старый вектор больше не валиден
    updated_at = NOW()
WHERE id = sqlc.arg(id)
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

-- name: GetUnindexedCatalogItems :many
SELECT 
    id AS catalog_id,
    standard_job_title,
    description
FROM 
    catalog_positions
WHERE 
    status = 'pending_indexing'
    AND kind = 'POSITION'
LIMIT $1;

-- name: SetCatalogStatusActive :exec
-- (Для RAG-воркера) Устанавливает статус 'active' после индексации
UPDATE catalog_positions
SET status = 'active'
WHERE id = ANY($1::bigint[]);

-- name: GetActiveCatalogItems :many
SELECT 
    id AS catalog_id,
    standard_job_title,
    description
FROM 
    catalog_positions
WHERE 
    status = 'active'
    AND kind = 'POSITION'
ORDER BY 
    id
LIMIT $1
OFFSET $2;

-- name: HybridSearchCatalogPositions :many
-- Гибридный поиск (RRF) для матчинга тендерных позиций.
-- $1 - вектор запроса (от Google), $2 - текст запроса (леммы от spaCy)
WITH semantic_search AS (
    SELECT id, row_number() OVER (ORDER BY embedding <=> $1) as rank
    FROM catalog_positions
    WHERE kind = 'POSITION' AND status = 'active'
    ORDER BY embedding <=> $1
    LIMIT 50
),
keyword_search AS (
    SELECT id, row_number() OVER (ORDER BY ts_rank(fts_vector, plainto_tsquery('simple', $2)) DESC) as rank
    FROM catalog_positions
    WHERE fts_vector @@ plainto_tsquery('simple', $2)
      AND kind = 'POSITION' AND status = 'active'
    LIMIT 50
)
SELECT 
    cp.id, 
    cp.standard_job_title, 
    cp.description, 
    cp.unit_id,
    -- Формула RRF
    (COALESCE(1.0 / (60 + s.rank), 0) + COALESCE(1.0 / (60 + k.rank), 0)) AS rrf_score
FROM semantic_search s
FULL OUTER JOIN keyword_search k ON s.id = k.id
JOIN catalog_positions cp ON cp.id = COALESCE(s.id, k.id)
ORDER BY rrf_score DESC
LIMIT $3;

-- name: ListPositionsForIndexing :many
-- Выбирает позиции, которые нужно обработать воркеру
SELECT id, standard_job_title, description
FROM catalog_positions
WHERE status = 'pending_indexing'
  AND kind = 'POSITION'
LIMIT $1;