-- tender_types.sql
-- Этот файл содержит набор SQL-запросов для управления типами тендеров,
-- которые являются верхним уровнем в трехуровневой иерархии справочников.

-- name: CreateTenderType :one
-- Создает новый тип тендера.
-- Поле `title` должно быть уникальным, иначе база данных вернет ошибку.
-- Возвращает полную запись созданного типа.
INSERT INTO tender_types (
    title
) VALUES (
    $1
)
RETURNING *;

-- name: GetTenderTypeByID :one
-- Получает один тип тендера по его уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM tender_types
WHERE id = $1;

-- name: GetTenderTypeByTitle :one
-- Получает один тип тендера по его уникальному наименованию (title).
-- Запрос очень быстрый, так как использует уникальный индекс по полю `title`.
SELECT * FROM tender_types
WHERE title = $1;

-- name: UpsertTenderType :one
-- Создает новый тип тендера или возвращает существующий, если он уже есть.
-- Этот запрос реализует логику "найти или создать" (get or create) в одной
-- атомарной операции. Используется трюк: `DO UPDATE` с присваиванием того же значения
-- позволяет использовать `RETURNING *` даже для уже существующих записей.
INSERT INTO tender_types (title)
VALUES ($1)
ON CONFLICT (title) DO UPDATE
SET title = EXCLUDED.title
RETURNING *;


-- name: ListTenderTypes :many
-- Получает пагинированный список всех типов тендеров.
-- Сортировка по `title` эффективна, так как это поле проиндексировано.
SELECT * FROM tender_types
ORDER BY title
LIMIT $1
OFFSET $2;

-- name: UpdateTenderType :one
-- Обновляет наименование существующего типа тендера по его ID.
UPDATE tender_types
SET
    title = $2,
    updated_at = NOW()
WHERE
    id = $1
RETURNING *;

-- name: DeleteTenderType :exec
-- Удаляет тип тендера по ID.
-- ВНИМАНИЕ: Операция УСПЕШНО выполнится и вызовет МАСШТАБНОЕ КАСКАДНОЕ УДАЛЕНИЕ.
-- Побочный эффект: будут удалены все дочерние РАЗДЕЛЫ (`tender_chapters`),
-- которые ссылались на этот тип, а затем будут удалены все дочерние КАТЕГОРИИ
-- (`tender_categories`), которые принадлежали этим разделам (согласно правилу ON DELETE CASCADE).
-- Это самая мощная операция удаления во всей иерархии справочников.
DELETE FROM tender_types
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateTenderTypeParams struct {
    Title string `json:"title"`
}

type UpsertTenderTypeParams struct {
    Title string `json:"title"`
}

type UpdateTenderTypeParams struct {
    ID    int64  `json:"id"`
    Title string `json:"title"`
}
*/