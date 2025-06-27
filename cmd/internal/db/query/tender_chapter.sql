-- tender_chapters.sql
-- Этот файл содержит набор SQL-запросов для управления разделами тендеров,
-- которые являются средним уровнем в трехуровневой иерархии справочников.

-- name: CreateTenderChapter :one
-- Создает новый раздел тендера, привязанный к типу тендера.
-- Поле `title` должно быть уникальным, иначе база данных вернет ошибку.
INSERT INTO tender_chapters (
    title,
    tender_type_id
) VALUES (
    $1, $2
)
RETURNING *;

-- name: GetTenderChapterByID :one
-- Получает один раздел по его уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM tender_chapters
WHERE id = $1;

-- name: GetTenderChapterByTitle :one
-- Получает один раздел по его уникальному наименованию (title).
-- Запрос очень быстрый, так как использует уникальный индекс по полю `title`.
SELECT * FROM tender_chapters
WHERE title = $1;

-- name: UpsertTenderChapter :one
-- Создает новый раздел или обновляет его привязку к типу (tender_type_id),
-- если раздел с таким title уже существует (Upsert).
INSERT INTO tender_chapters (
    title,
    tender_type_id
) VALUES (
    $1, $2
)
ON CONFLICT (title) DO UPDATE SET
    tender_type_id = EXCLUDED.tender_type_id,
    updated_at = NOW()
RETURNING *;

-- name: ListTenderChapters :many
-- Получает обогащенный пагинированный список всех разделов.
-- Запрос через JOIN сразу подтягивает название родительского типа,
-- что позволяет избежать дополнительных запросов к БД из приложения (N+1 проблема).
SELECT
    tc.id,
    tc.title,
    tc.tender_type_id,
    tt.title as tender_type_title,
    tc.created_at
FROM
    tender_chapters tc
JOIN
    tender_types tt ON tc.tender_type_id = tt.id
ORDER BY
    tt.title, tc.title
LIMIT $1
OFFSET $2;

-- name: ListTenderChaptersByType :many
-- Получает пагинированный список всех разделов, принадлежащих одному типу.
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (НА БУДУЩЕЕ) ###############
-- ДАННЫЙ ЗАПРОС БУДЕТ РАБОТАТЬ МЕДЛЕННО на больших объемах данных.
-- Причина: отсутствует индекс по колонке `tender_type_id`.
-- Решение: В будущей миграции добавить следующий индекс:
-- CREATE INDEX idx_tender_chapters_type_id ON tender_chapters (tender_type_id);
-- #####################################################################
SELECT * FROM tender_chapters
WHERE tender_type_id = $1
ORDER BY title
LIMIT $2
OFFSET $3;

-- name: UpdateTenderChapter :one
-- Обновляет существующий раздел тендера по его ID.
-- Запрос использует паттерн COALESCE, что позволяет обновлять только те поля,
-- для которых были переданы не-NULL значения, делая API гибким.
UPDATE tender_chapters
SET
    title = COALESCE(sqlc.narg(title), title),
    tender_type_id = COALESCE(sqlc.narg(tender_type_id), tender_type_id),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTenderChapter :exec
-- Удаляет раздел тендера по ID.
-- ВНИМАНИЕ: Операция УСПЕШНО выполнится и вызовет КАСКАДНОЕ УДАЛЕНИЕ.
-- Побочный эффект: будут удалены все дочерние КАТЕГОРИИ (`tender_categories`),
-- которые ссылались на этот раздел (согласно правилу ON DELETE CASCADE).
-- Это мощный, но потенциально разрушительный механизм.
DELETE FROM tender_chapters
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateTenderChapterParams struct {
    Title        string `json:"title"`
    TenderTypeID int64  `json:"tender_type_id"`
}

type ListTenderChaptersRow struct {
	ID             int64     `json:"id"`
	Title          string    `json:"title"`
	TenderTypeID   int64     `json:"tender_type_id"`
	TenderTypeTitle string   `json:"tender_type_title"`
	CreatedAt      time.Time `json:"created_at"`
}

type UpdateTenderChapterParams struct {
    ID           int64          `json:"id"`
    Title        sql.NullString `json:"title"`
    TenderTypeID sql.NullInt64  `json:"tender_type_id"`
}
*/