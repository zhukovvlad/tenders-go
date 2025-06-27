-- tender_categories.sql
-- Этот файл содержит набор SQL-запросов для управления категориями тендеров,
-- которые являются нижним уровнем в трехуровневой иерархии справочников.

-- name: CreateTenderCategory :one
-- Создает новую категорию тендера, привязанную к разделу тендера.
-- Поле `title` должно быть уникальным, иначе база данных вернет ошибку.
INSERT INTO tender_categories (
    title,
    tender_chapter_id
) VALUES (
    $1, $2
)
RETURNING *;

-- name: GetTenderCategoryByID :one
-- Получает одну категорию по ее уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM tender_categories
WHERE id = $1;

-- name: GetTenderCategoryByTitle :one
-- Получает одну категорию по ее уникальному наименованию (title).
-- Запрос очень быстрый, так как использует уникальный индекс по полю `title`.
SELECT * FROM tender_categories
WHERE title = $1;

-- name: UpsertTenderCategory :one
-- Создает новую категорию или обновляет ее привязку к разделу (tender_chapter_id),
-- если категория с таким title уже существует (Upsert).
INSERT INTO tender_categories (
    title,
    tender_chapter_id
) VALUES (
    $1, $2
)
ON CONFLICT (title) DO UPDATE SET
    tender_chapter_id = EXCLUDED.tender_chapter_id,
    updated_at = NOW()
RETURNING *;

-- name: ListTenderCategories :many
-- Получает обогащенный пагинированный список всех категорий.
-- Запрос через JOIN сразу подтягивает название родительского раздела и ID типа,
-- что позволяет избежать дополнительных запросов к БД из приложения (N+1 проблема).
SELECT
  tc.id,
  tc.title,
  tc.tender_chapter_id,
  tch.title AS tender_chapter_title,
  tch.tender_type_id
FROM
  tender_categories tc
JOIN
  tender_chapters tch ON tc.tender_chapter_id = tch.id
ORDER BY
  tc.title
LIMIT $1
OFFSET $2;

-- name: ListTenderCategoriesByChapter :many
-- Получает пагинированный список всех категорий, принадлежащих одному разделу.
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (НА БУДУЩЕЕ) ###############
-- ДАННЫЙ ЗАПРОС БУДЕТ РАБОТАТЬ МЕДЛЕННО на больших объемах данных.
-- Причина: отсутствует индекс по колонке `tender_chapter_id`.
-- Решение: В будущей миграции добавить следующий индекс:
-- CREATE INDEX idx_tender_categories_chapter_id ON tender_categories (tender_chapter_id);
-- #####################################################################
SELECT * FROM tender_categories
WHERE tender_chapter_id = $1
ORDER BY title
LIMIT $2
OFFSET $3;

-- name: UpdateTenderCategory :one
-- Обновляет существующую категорию тендера по ее ID.
-- Запрос использует паттерн COALESCE, что позволяет обновлять только те поля,
-- для которых были переданы не-NULL значения, делая API гибким.
UPDATE tender_categories
SET
    title = COALESCE(sqlc.narg(title), title),
    tender_chapter_id = COALESCE(sqlc.narg(tender_chapter_id), tender_chapter_id),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTenderCategory :exec
-- Удаляет категорию тендера по ID.
-- ВНИМАНИЕ: Операция УСПЕШНО выполнится, даже если на категорию ссылаются тендеры.
-- Побочный эффект: у всех тендеров, которые использовали эту категорию, поле
-- `category_id` будет автоматически установлено в NULL (согласно правилу ON DELETE SET NULL).
DELETE FROM tender_categories
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateTenderCategoryParams struct {
    Title           string `json:"title"`
    TenderChapterID int64  `json:"tender_chapter_id"`
}

type ListTenderCategoriesRow struct {
	ID                 int64  `json:"id"`
	Title              string `json:"title"`
	TenderChapterID    int64  `json:"tender_chapter_id"`
	TenderChapterTitle string `json:"tender_chapter_title"`
	TenderTypeID       int64  `json:"tender_type_id"`
}

type UpdateTenderCategoryParams struct {
    ID              int64          `json:"id"`
    Title           sql.NullString `json:"title"`
    TenderChapterID sql.NullInt64  `json:"tender_chapter_id"`
}
*/