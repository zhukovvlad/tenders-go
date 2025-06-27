-- lot.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над таблицей 'lots'.
-- Запросы написаны с учетом производительности и безопасности для использования с sqlc.

-- name: UpsertLot :one
-- Создает новый лот или обновляет существующий, если лот с парой (tender_id, lot_key) уже существует.
-- Этот подход "создай или обнови" (Upsert) является атомарным и эффективным.
-- Он использует уникальный индекс по (tender_id, lot_key) для определения конфликта.
-- При обновлении (ON CONFLICT) поля lot_title и lot_key_parameters берутся из новых,
-- переданных в запрос значений (через виртуальную таблицу EXCLUDED).
-- Возвращает полную запись созданного или обновленного лота.
INSERT INTO lots (
    tender_id,
    lot_key,
    lot_title,
    lot_key_parameters
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (tender_id, lot_key) DO UPDATE SET
    lot_title = EXCLUDED.lot_title,
    lot_key_parameters = EXCLUDED.lot_key_parameters,
    updated_at = NOW()
RETURNING *;

-- name: GetLotByID :one
-- Получает одну запись лота по его уникальному внутреннему идентификатору (primary key).
-- Это основной способ точечного получения лота. Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM lots
WHERE id = $1;

-- name: GetLotByTenderAndKey :one
-- Получает одну запись лота по его "бизнес-ключу" - комбинации ID тендера и ключа лота.
-- Запрос очень быстрый, так как использует уникальный композитный индекс по (tender_id, lot_key).
SELECT * FROM lots
WHERE tender_id = $1 AND lot_key = $2;

-- name: ListLotsByTenderID :many
-- Получает пагинированный список всех лотов, принадлежащих одному тендеру.
-- Это основной способ отображения лотов на странице тендера.
-- Параметры: $1 - ID тендера, $2 - LIMIT (лимит записей на странице), $3 - OFFSET (смещение).
-- Сортировка по lot_key эффективна, т.к. это поле является частью индекса.
SELECT * FROM lots
WHERE tender_id = $1
ORDER BY lot_key
LIMIT $2
OFFSET $3;

-- name: ListAllLots :many
-- Получает пагинированный список абсолютно всех лотов в системе.
-- Менее частый запрос, может использоваться для административных панелей или отчетов.
-- Сортировка по (tender_id, lot_key) максимально эффективна, так как полностью
-- соответствует композитному уникальному индексу, что избавляет БД от операции сортировки.
SELECT * FROM lots
ORDER BY tender_id, lot_key
LIMIT $1
OFFSET $2;

-- name: UpdateLotDetails :one
-- Обновляет детали существующего лота по его внутреннему ID.
-- Запрос использует паттерн COALESCE(sqlc.narg(...), ...), что позволяет обновлять
-- только те поля, для которых были переданы не-NULL значения. Это делает API гибким.
-- Предостережение: обновление полей tender_id и lot_key может привести к ошибке нарушения
-- уникальности, если новая пара (tender_id, lot_key) уже существует в таблице.
UPDATE lots
SET
    tender_id = COALESCE(sqlc.narg(tender_id), tender_id),
    lot_key = COALESCE(sqlc.narg(lot_key), lot_key),
    lot_title = COALESCE(sqlc.narg(lot_title), lot_title),
    lot_key_parameters = COALESCE(sqlc.narg(lot_key_parameters), lot_key_parameters),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteLot :exec
-- Удаляет лот по его внутреннему ID.
-- ВНИМАНИЕ: Эта операция запускает каскадное удаление (ON DELETE CASCADE).
-- Удаление лота приведет к автоматическому удалению всех связанных с ним записей в таблицах:
-- 1. proposals (все предложения подрядчиков по этому лоту)
-- 2. lots_md_documents (вся документация по лоту)
-- 3. lots_chunks (все чанки и эмбеддинги, связанные с документацией)
-- Это мощный, но потенциально разрушительный механизм. Используйте с осторожностью.
DELETE FROM lots
WHERE id = $1;

-- -- name: ListLotsForTender :many
-- ЗАПРОС ЗАКОММЕНТИРОВАН И УДАЛЕН ИЗ-ЗА НЕБЕЗОПАСНОСТИ.
-- Этот запрос не имел пагинации, что создавало риск исчерпания памяти сервера
-- и DoS-атаки при большом количестве лотов у одного тендера.
-- Вместо него всегда следует использовать пагинированный `ListLotsByTenderID`.
-- SELECT * FROM lots
-- WHERE tender_id = $1
-- ORDER BY id;

-- name: CountLotsForTender :one
-- Эффективно подсчитывает общее количество лотов для указанного тендера.
-- Этот запрос необходим для построения метаданных пагинации в API.
-- Он позволяет UI отображать информацию вида "Страница 2 из 15".
-- Работает быстро, так как использует индекс по полю tender_id.
SELECT count(*) FROM lots
WHERE tender_id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertLotParams struct {
    TenderID          int64           `json:"tender_id"`
    LotKey            string          `json:"lot_key"`
    LotTitle          string          `json:"lot_title"`
    LotKeyParameters  json.RawMessage `json:"lot_key_parameters"`
}

type ListLotsByTenderIDParams struct {
    TenderID int64 `json:"tender_id"`
    Limit    int32 `json:"limit"`
    Offset   int32 `json:"offset"`
}

type UpdateLotDetailsParams struct {
    TenderID          sql.NullInt64   `json:"tender_id"`
    LotKey            sql.NullString  `json:"lot_key"`
    LotTitle          sql.NullString  `json:"lot_title"`
    LotKeyParameters  json.RawMessage `json:"lot_key_parameters"`
    ID                int64           `json:"id"`
}
*/