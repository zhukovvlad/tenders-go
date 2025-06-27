-- object.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над таблицей 'objects'.
-- Запросы написаны с учетом производительности и безопасности для использования с sqlc.

-- name: CreateObject :one
-- Создает новую запись об объекте в базе данных.
-- Поле `title` должно быть уникальным, иначе база данных вернет ошибку.
-- Поля `created_at` и `updated_at` устанавливаются автоматически значением по умолчанию (now()).
-- Возвращает полную запись созданного объекта.
INSERT INTO objects (
    title,
    address
) VALUES (
    $1, $2
)
RETURNING *;

-- name: GetObjectByID :one
-- Получает одну запись объекта по его уникальному внутреннему идентификатору (primary key).
-- Это основной и самый быстрый способ точечного получения объекта.
SELECT * FROM objects
WHERE id = $1;

-- name: GetObjectByTitle :one
-- Получает одну запись объекта по его уникальному наименованию (title).
-- Запрос очень быстрый, так как использует уникальный индекс по полю `title`.
SELECT * FROM objects
WHERE title = $1;

-- name: ListObjects :many
-- Получает пагинированный список всех объектов в системе.
-- Параметры: $1 - LIMIT (лимит записей на странице), $2 - OFFSET (смещение).
-- Сортировка по `title` эффективна, так как это поле проиндексировано.
SELECT * FROM objects
ORDER BY title
LIMIT $1
OFFSET $2;

-- name: UpdateObject :one
-- Обновляет детали существующего объекта по его внутреннему ID.
-- Запрос использует паттерн COALESCE(sqlc.narg(...), ...), что позволяет обновлять
-- только те поля, для которых были переданы не-NULL значения. Это делает API гибким.
-- Предостережение: обновление поля `title` может привести к ошибке нарушения
-- уникальности, если новое наименование уже существует у другого объекта.
UPDATE objects
SET
    title = COALESCE(sqlc.narg(title), title),
    address = COALESCE(sqlc.narg(address), address),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteObject :exec
-- Удаляет объект по его внутреннему ID.
-- ВНИМАНИЕ: Эта операция ЗАВЕРШИТСЯ С ОШИБКОЙ, если на этот объект ссылается
-- хотя бы одна запись из таблицы `tenders`.
-- Для внешнего ключа `tenders.object_id` действует правило `ON DELETE RESTRICT` (по умолчанию).
-- Это означает, что приложение должно само реализовать логику, запрещающую удаление
-- используемых объектов, и корректно обрабатывать ошибку нарушения целостности.
DELETE FROM objects
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateObjectParams struct {
    Title   string `json:"title"`
    Address string `json:"address"`
}

type UpdateObjectParams struct {
    ID      int64          `json:"id"`
    Title   sql.NullString `json:"title"`
    Address sql.NullString `json:"address"`
}
*/