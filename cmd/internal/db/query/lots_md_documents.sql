-- lots_md_documents.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- над таблицей 'lots_md_documents', которая хранит исходные текстовые
-- документы, связанные с лотами.

-- name: CreateLotsMdDocument :one
-- Создает новую запись для документа, связанного с лотом.
INSERT INTO lots_md_documents (
    lot_id,
    document_name,
    full_content
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: UpsertLotsMdDocument :one
-- Создает новый документ или обновляет существующий, если документ с парой
-- (lot_id, document_name) уже существует (Upsert).
-- ############### РЕКОМЕНДАЦИЯ ПО СХЕМЕ (ВАЖНО!) ###############
-- ДАННЫЙ ЗАПРОС ТРЕБУЕТ УНИКАЛЬНОГО ИНДЕКСА для корректной работы ON CONFLICT.
-- Решение: В будущей миграции добавить следующий уникальный индекс:
-- CREATE UNIQUE INDEX uq_lot_id_document_name ON lots_md_documents (lot_id, document_name);
-- Этот индекс также ускорит работу запроса GetLotsMdDocumentByLotAndName.
-- #####################################################################
INSERT INTO lots_md_documents (
    lot_id,
    document_name,
    full_content
) VALUES (
    $1, $2, $3
)
ON CONFLICT (lot_id, document_name) DO UPDATE SET
    full_content = EXCLUDED.full_content,
    updated_at = NOW()
RETURNING *;


-- name: GetLotsMdDocumentByID :one
-- Получает один документ по его уникальному внутреннему идентификатору (primary key).
SELECT * FROM lots_md_documents
WHERE id = $1;

-- name: GetLotsMdDocumentByLotAndName :one
-- Получает один документ по его "бизнес-ключу" - ID лота и имени документа.
-- Будет работать быстро после добавления уникального индекса, рекомендованного выше.
SELECT * FROM lots_md_documents
WHERE lot_id = $1 AND document_name = $2;


-- name: ListLotsMdDocumentsByLotID :many
-- Получает пагинированный список всех документов для указанного лота.
-- Запрос безопасен, так как использует пагинацию (LIMIT/OFFSET).
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (НА БУДУЩЕЕ) ###############
-- Производительность этого запроса зависит от индекса по `lot_id`.
-- Уникальный индекс, рекомендованный для `UpsertLotsMdDocument`,
-- (CREATE UNIQUE INDEX ... ON lots_md_documents (lot_id, document_name))
-- автоматически покроет и этот случай, сделав запрос быстрым.
-- #####################################################################
SELECT
    id,
    lot_id,
    document_name,
    -- Не выбираем `full_content` для списков, чтобы не передавать мегабайты текста
    LENGTH(full_content) as content_length,
    created_at,
    updated_at
FROM lots_md_documents
WHERE lot_id = $1
ORDER BY document_name
LIMIT $2
OFFSET $3;

-- name: UpdateLotsMdDocument :one
-- Обновляет детали существующего документа по его ID.
-- Позволяет гибко обновлять имя или содержимое документа.
UPDATE lots_md_documents
SET
    document_name = COALESCE(sqlc.narg(document_name), document_name),
    full_content = COALESCE(sqlc.narg(full_content), full_content),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteLotsMdDocument :exec
-- Удаляет один документ по его внутреннему ID.
-- ВНИМАНИЕ: это приведет к каскадному удалению (`ON DELETE CASCADE`)
-- всех связанных чанков в таблице `lots_chunks`.
DELETE FROM lots_md_documents
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertLotsMdDocumentParams struct {
	LotID         int64  `json:"lot_id"`
	DocumentName  string `json:"document_name"`
	FullContent   string `json:"full_content"`
}

type ListLotsMdDocumentsRow struct {
	ID            int64     `json:"id"`
	LotID         int64     `json:"lot_id"`
	DocumentName  string    `json:"document_name"`
	ContentLength int32     `json:"content_length"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
*/