-- units_of_measurement.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над справочником единиц измерения.

-- name: CreateUnitOfMeasurement :one
-- Создает новую запись для единицы измерения.
-- Поле `normalized_name` должно быть уникальным, иначе база данных вернет ошибку.
-- Поля `full_name` и `description` могут быть NULL.
-- Возвращает полную запись созданной единицы измерения.
INSERT INTO units_of_measurement (
    normalized_name,
    full_name,
    description
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetUnitOfMeasurementByID :one
-- Получает одну единицу измерения по её уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM units_of_measurement
WHERE id = $1;

-- name: GetUnitOfMeasurementByNormalizedName :one
-- Получает одну единицу измерения по её уникальному нормализованному наименованию.
-- Запрос очень быстрый, так как использует уникальный индекс по полю `normalized_name`.
-- Удобен для логики "найти или создать" на стороне приложения.
SELECT * FROM units_of_measurement
WHERE normalized_name = $1;

-- name: ListUnitsOfMeasurement :many
-- Получает пагинированный список всех единиц измерения.
-- Сортировка по `normalized_name` эффективна, так как это поле проиндексировано.
SELECT * FROM units_of_measurement
ORDER BY normalized_name
LIMIT $1
OFFSET $2;

-- name: UpdateUnitOfMeasurement :one
-- Обновляет существующую единицу измерения по ее ID.
-- Запрос использует паттерн COALESCE, что позволяет обновлять только те поля,
-- для которых были переданы не-NULL значения, делая API гибким.
UPDATE units_of_measurement
SET
    normalized_name = COALESCE(sqlc.narg(normalized_name), normalized_name),
    full_name = COALESCE(sqlc.narg(full_name), full_name),
    description = COALESCE(sqlc.narg(description), description),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUnitOfMeasurement :exec
-- Удаляет единицу измерения по ID.
-- ############### ЗАМЕЧАНИЕ ПО ЛОГИКЕ (ВАЖНО!) ###############
-- ДАННАЯ ОПЕРАЦИЯ ЗАВЕРШИТСЯ С ОШИБКОЙ, если эта единица измерения используется
-- хотя бы в одной записи в таблице `position_items`.
-- Причина: для внешнего ключа `position_items.unit_id` действует правило `ON DELETE RESTRICT`.
-- Это безопасное поведение. Приложение должно обрабатывать эту ошибку и запрещать
-- удаление используемых единиц измерения.
-- #####################################################################
DELETE FROM units_of_measurement
WHERE id = $1;

/*
Пример того, как sqlc сгенерирует параметры для CreateUnitOfMeasurement:

type CreateUnitOfMeasurementParams struct {
    NormalizedName string         `json:"normalized_name"`
    FullName       sql.NullString `json:"full_name"`
    Description    sql.NullString `json:"description"`
}

Аналогично для UpdateUnitOfMeasurementParams:

type UpdateUnitOfMeasurementParams struct {
    ID             int64          `json:"id"`
    NormalizedName sql.NullString `json:"normalized_name"`
    FullName       sql.NullString `json:"full_name"`
    Description    sql.NullString `json:"description"`
}
*/