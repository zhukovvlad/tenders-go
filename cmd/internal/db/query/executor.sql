-- executor.sql

-- name: CreateExecutor :one
-- Создает нового исполнителя.
-- name должен быть уникальным.
-- created_at и updated_at будут установлены по умолчанию (DEFAULT now()).
INSERT INTO executors (
    name,
    phone
) VALUES (
    $1, $2
)
RETURNING *;

-- name: GetExecutorByID :one
-- Получает исполнителя по его ID.
SELECT * FROM executors
WHERE id = $1;

-- name: GetExecutorByName :one
-- Получает исполнителя по его имени (name), которое уникально.
SELECT * FROM executors
WHERE name = $1;

-- name: ListExecutors :many
-- Получает список всех исполнителей с пагинацией.
SELECT * FROM executors
ORDER BY name -- или id, или created_at
LIMIT $1
OFFSET $2;

-- name: UpdateExecutor :one
-- Обновляет существующего исполнителя. Поля обновляются только если передано НЕ NULL значение.
UPDATE executors
SET
    name = COALESCE(sqlc.narg(name), name),
    phone = COALESCE(sqlc.narg(phone), phone),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteExecutor :exec
-- Удаляет исполнителя по ID.
DELETE FROM executors
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateExecutorParams struct {
    Name  string `json:"name"`
    Phone string `json:"phone"`
}

type UpdateExecutorParams struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Phone string `json:"phone"`
}
*/