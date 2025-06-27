-- catalog_positions.sql

-- name: CreateCatalogPosition :one
-- Создает новую запись в каталоге стандартных позиций.
-- standard_job_title должен быть уникальным.
-- description может быть NULL. Поле embedding изначально будет NULL.
-- created_at и updated_at будут установлены по умолчанию (DEFAULT now()).
INSERT INTO catalog_positions (
    standard_job_title,
    description
) VALUES (
    $1, $2
)
RETURNING *;

-- name: GetCatalogPositionByID :one
-- Получает стандартную позицию по её ID.
SELECT * FROM catalog_positions
WHERE id = $1;

-- name: GetCatalogPositionByStandardJobTitle :one
-- Получает стандартную позицию по её уникальному standard_job_title.
-- Удобно для проверки существования ("найти или создать").
SELECT * FROM catalog_positions
WHERE standard_job_title = $1;

-- name: ListCatalogPositions :many
-- Получает список всех стандартных позиций с пагинацией.
SELECT * FROM catalog_positions
ORDER BY standard_job_title -- или id
LIMIT $1
OFFSET $2;

-- name: ListCatalogPositionsForEmbedding :many
-- Получает список стандартных позиций, для которых эмбеддинг еще не сгенерирован (NULL).
-- Используется для процесса генерации эмбеддингов.
-- Возвращает только необходимые поля для генерации эмбеддинга.
SELECT id, standard_job_title, description FROM catalog_positions
WHERE embedding IS NULL
ORDER BY id -- Для последовательной обработки
LIMIT $1 -- Можно обрабатывать пачками
OFFSET $2;

-- name: UpdateCatalogPositionDetails :one
-- Обновляет текстовые детали существующей стандартной позиции (standard_job_title, description).
-- При этом ОБНУЛЯЕТ существующий эмбеддинг, так как он становится неактуальным.
-- standard_job_title должен оставаться уникальным.
-- updated_at обновляется на NOW().
-- Поля обновляются только если передано НЕ NULL значение.
UPDATE catalog_positions
SET
    -- COALESCE(новое_значение, старое_значение)
    standard_job_title = COALESCE(sqlc.narg(standard_job_title), standard_job_title),
    description = COALESCE(sqlc.narg(description), description),
    embedding = NULL,
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: UpdateCatalogPositionEmbedding :one
-- Обновляет поле embedding для существующей стандартной позиции.
-- Используется после генерации эмбеддинга.
-- updated_at обновляется на NOW().
UPDATE catalog_positions
SET
    embedding = sqlc.arg(embedding),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCatalogPosition :exec
-- Удаляет стандартную позицию по ID.
-- Убедитесь, что на эту позицию нет ссылок из position_items, или настройте ON DELETE.
DELETE FROM catalog_positions
WHERE id = $1;

-- name: SearchCatalogPositionsByTitle :many
-- Ищет стандартные позиции по частичному совпадению standard_job_title, без учета регистра.
SELECT * FROM catalog_positions
WHERE standard_job_title ILIKE '%' || sqlc.arg(search_term)::text || '%'
ORDER BY standard_job_title
LIMIT sqlc.arg(page_limit)::int
OFFSET sqlc.arg(page_offset)::int;

/*
Примерные структуры параметров и возвращаемых типов для некоторых функций:

type CreateCatalogPositionParams struct {
    StandardJobTitle string         `json:"standard_job_title"`
    Description      sql.NullString `json:"description"`
}

type UpdateCatalogPositionDetailsParams struct {
    ID               int64          `json:"id"`
    StandardJobTitle string         `json:"standard_job_title"`
    Description      sql.NullString `json:"description"`
}

type UpdateCatalogPositionEmbeddingParams struct {
    ID        int64  `json:"id"`
    Embedding []byte `json:"embedding"` // bytea обычно мапится в []byte, которое может быть nil
}

type ListCatalogPositionsForEmbeddingRow struct {
    ID               int64          `json:"id"`
    StandardJobTitle string         `json:"standard_job_title"`
    Description      sql.NullString `json:"description"`
}
*/