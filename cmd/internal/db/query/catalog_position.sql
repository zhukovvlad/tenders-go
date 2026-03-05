-- catalog_position.sql
-- (Версия 3.1, исправленная, с атомарным обновлением статуса)

-- name: CreateCatalogPosition :one
-- Создает позицию или обновляет существующую (Upsert).
-- ВАЖНО: Всегда ставит status = 'pending_indexing', чтобы воркер создал эмбеддинг.
INSERT INTO catalog_positions (
    standard_job_title,
    description,
    kind,
    status,
    unit_id
) VALUES (
    $1, -- standard_job_title
    $2, -- description
    $3, -- kind
    'pending_indexing', -- STATUS: Всегда отправляем в очередь на векторизацию
    $4  -- unit_id
)
ON CONFLICT (standard_job_title, COALESCE(unit_id, -1)) 
DO UPDATE SET
    description = EXCLUDED.description,
    kind = EXCLUDED.kind,
    status = 'pending_indexing',        -- Сбрасываем статус, чтобы обновить вектор
    embedding = NULL,                   -- Сбрасываем старый вектор
    updated_at = NOW()
RETURNING *;

-- name: GetCatalogPositionByID :one
SELECT * FROM catalog_positions
WHERE id = $1;

-- name: GetCatalogPositionByTitleAndUnit :one
SELECT * FROM catalog_positions
WHERE standard_job_title = $1
  AND (unit_id = sqlc.narg('unit_id') OR (unit_id IS NULL AND sqlc.narg('unit_id') IS NULL));

-- name: ListCatalogPositions :many
SELECT id, standard_job_title, description, kind, created_at, updated_at
FROM catalog_positions
ORDER BY standard_job_title
LIMIT $1
OFFSET $2;

-- name: ListCatalogPositionsForEmbedding :many
-- Основная очередь для воркера. Ищем по статусу.
SELECT id, standard_job_title, description 
FROM catalog_positions
WHERE 
    status = 'pending_indexing'
    AND kind = 'POSITION'
ORDER BY id
LIMIT $1;

-- name: UpdateCatalogPositionDetails :one
-- Обновляет детали, сбрасывает статус и удаляет старый вектор.
UPDATE catalog_positions
SET
    standard_job_title = COALESCE(sqlc.narg(standard_job_title), standard_job_title),
    description = COALESCE(sqlc.narg(description), description),
    unit_id = COALESCE(sqlc.narg(unit_id), unit_id),
    status = 'pending_indexing',
    embedding = NULL,
    updated_at = NOW()
WHERE id = sqlc.arg(id)
  AND (
    (sqlc.narg(standard_job_title) IS NOT NULL AND sqlc.narg(standard_job_title) IS DISTINCT FROM standard_job_title)
    OR (sqlc.narg(description) IS NOT NULL AND sqlc.narg(description) IS DISTINCT FROM description)
    OR (sqlc.narg(unit_id) IS NOT NULL AND sqlc.narg(unit_id) IS DISTINCT FROM unit_id)
  )
RETURNING *;

-- name: UpdateCatalogPositionEmbedding :one
-- ВАЖНО: Обновляет эмбеддинг и СРАЗУ активирует позицию (атомарность).
UPDATE catalog_positions
SET
    embedding = sqlc.arg(embedding),
    status = 'active',
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCatalogPosition :exec
DELETE FROM catalog_positions
WHERE id = $1;

-- name: SearchCatalogPositions :many
-- Простой поиск для админки по VIEW.
SELECT id, standard_job_title, description, kind, created_at, updated_at
FROM catalog_positions_clean
WHERE standard_job_title ILIKE '%' || sqlc.arg(search_term)::text || '%'
ORDER BY standard_job_title
LIMIT sqlc.arg(page_limit)::int
OFFSET sqlc.arg(page_offset)::int;

-- name: ListCatalogPositionsToReview :many
SELECT * FROM catalog_positions
WHERE kind = 'TO_REVIEW'
ORDER BY created_at DESC
LIMIT $1
OFFSET $2;

-- name: SetCatalogStatusActive :exec
-- Оставляем на случай массовой активации, если понадобится.
UPDATE catalog_positions
SET status = 'active'
WHERE id = ANY($1::bigint[]);

-- name: GetActiveCatalogItems :many
-- Пагинация активных позиций каталога (для поиска дубликатов и др.)
SELECT id AS catalog_id, standard_job_title, description
FROM catalog_positions
WHERE kind = 'POSITION' AND status = 'active'
ORDER BY id
LIMIT $1
OFFSET $2;

-- name: HybridSearchCatalogPositions :many
-- Гибридный поиск (RRF) для матчинга.
WITH semantic_search AS (
    SELECT id, row_number() OVER (ORDER BY embedding <=> $1::vector) as rank
    FROM catalog_positions
    WHERE kind = 'POSITION' 
      AND status = 'active' 
      AND embedding IS NOT NULL
    ORDER BY embedding <=> $1::vector
    LIMIT 50
),
keyword_search AS (
    SELECT id, row_number() OVER (ORDER BY ts_rank(fts_vector, plainto_tsquery('simple', $2)) DESC) as rank
    FROM catalog_positions
    WHERE fts_vector @@ plainto_tsquery('simple', $2)
      AND kind = 'POSITION' 
      AND status = 'active'
    ORDER BY ts_rank(fts_vector, plainto_tsquery('simple', $2)) DESC
    LIMIT 50
)
SELECT 
    cp.id, 
    cp.standard_job_title, 
    cp.description, 
    cp.unit_id,
    -- Кастинг в float8 для Go
    (COALESCE(1.0 / (60 + s.rank), 0.0) + COALESCE(1.0 / (60 + k.rank), 0.0))::float8 AS rrf_score 
FROM semantic_search s
FULL OUTER JOIN keyword_search k ON s.id = k.id
JOIN catalog_positions cp ON cp.id = COALESCE(s.id, k.id)
ORDER BY rrf_score DESC
LIMIT $3;

-- name: MergeCatalogPosition :one
-- Выполняет слияние: помечает дубликат как влитый в мастер-позицию.
-- Устанавливает merged_into_id и меняет статус на 'deprecated'.
-- Дополнительно проверяет, что мастер-позиция активна и не влита в другую.
UPDATE catalog_positions dup
SET
    merged_into_id = sqlc.arg(master_id),
    status = 'deprecated',
    updated_at = NOW()
WHERE
    dup.id = sqlc.arg(duplicate_id)
    AND dup.merged_into_id IS NULL      -- Защита: нельзя повторно влить дубликат
    AND dup.status != 'deprecated'      -- Явная защита: нельзя влить уже deprecated-позицию
    AND EXISTS (                        -- Защита: мастер должен быть активен и не влит
        SELECT 1 FROM catalog_positions master
        WHERE master.id = sqlc.arg(master_id)
          AND master.merged_into_id IS NULL
          AND master.status = 'active'
    )
RETURNING dup.*;

-- name: GetMergedPositions :many
-- Возвращает все позиции, влитые в указанную мастер-позицию.
SELECT * FROM catalog_positions
WHERE merged_into_id = $1
ORDER BY id;

-- name: CreateSimpleCatalogPosition :one
-- Создает НОВУЮ каталожную позицию с минимальными параметрами (для Merge-to-New).
-- В отличие от CreateCatalogPosition, не использует UPSERT — создаёт уникальную запись.
-- Статус 'pending_indexing' — позже RAG-воркер создаст эмбеддинг.
INSERT INTO catalog_positions (
    standard_job_title,
    kind,
    status
) VALUES (
    $1,                  -- standard_job_title (новое имя из оператора)
    'POSITION',          -- kind: всегда POSITION
    'pending_indexing'   -- status: в очередь на индексацию
)
RETURNING *;

-- name: SetPositionMerged :one
-- Устанавливает merged_into_id и меняет статус на 'deprecated' (для Merge-to-New).
-- Упрощённая версия MergeCatalogPosition: не проверяет статус целевой позиции,
-- т.к. целевая (C) только что создана и может быть в pending_indexing.
-- Защита: нельзя повторно влить уже deprecated-позицию.
UPDATE catalog_positions
SET
    merged_into_id = sqlc.arg(target_id),
    status = 'deprecated',
    updated_at = NOW()
WHERE
    id = sqlc.arg(position_id)
    AND merged_into_id IS NULL
    AND status != 'deprecated'
RETURNING *;

-- name: FlattenMergeChain :exec
-- Перевешивает все позиции, ранее влитые в old_master_id, напрямую на new_master_id.
-- Выполняет "сжатие путей" (Path Compression) для предотвращения транзитивных цепочек.
UPDATE catalog_positions
SET
    merged_into_id = sqlc.arg(new_master_id),
    updated_at = NOW()
WHERE
    merged_into_id = sqlc.arg(old_master_id);

-- name: CreateParentCatalogPosition :one
-- Создает абстрактную родительскую позицию (HEADER).
INSERT INTO catalog_positions (
    standard_job_title,
    kind,
    status
) VALUES (
    $1,                  -- standard_job_title
    'HEADER',            -- kind: абстрактная группа
    'active'             -- status: сразу активна, так как это просто родитель
)
RETURNING *;

-- name: CountPositionsByParentID :one
-- Считает количество позиций, привязанных к данному родителю.
SELECT COUNT(*) FROM catalog_positions WHERE parent_id = $1;

-- name: SetPositionParent :one
-- Привязывает позицию к родителю. Позиция остается активной.
UPDATE catalog_positions
SET
    parent_id = sqlc.arg(parent_id),
    updated_at = NOW()
WHERE
    id = sqlc.arg(position_id)
    AND merged_into_id IS NULL
    AND status != 'deprecated'
RETURNING *;