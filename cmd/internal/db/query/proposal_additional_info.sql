-- proposal_additional_info.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над таблицей 'proposal_additional_info'.
-- Таблица хранит произвольные данные в формате "ключ-значение", связанные с предложением.

-- name: UpsertProposalAdditionalInfo :one
-- Создает новую запись доп. информации или обновляет существующую,
-- если запись с парой (proposal_id, info_key) уже существует (Upsert).
-- Этот подход является атомарным и основным для импорта данных.
-- Возвращает полную запись созданного или обновленного объекта.
INSERT INTO proposal_additional_info (
    proposal_id,
    info_key,
    info_value
) VALUES (
    $1, $2, $3
)
ON CONFLICT (proposal_id, info_key) DO UPDATE SET
    info_value = EXCLUDED.info_value,
    updated_at = NOW()
RETURNING *;

-- name: GetProposalAdditionalInfoByKey :one
-- Получает одну запись доп. информации по ее "бизнес-ключу" - комбинации ID предложения и ключа.
-- Запрос очень быстрый, так как использует уникальный композитный индекс.
SELECT * FROM proposal_additional_info
WHERE proposal_id = $1 AND info_key = $2;

-- name: ListProposalAdditionalInfoByProposalID :many
-- Получает пагинированный список всех записей доп. информации для указанного proposal_id.
-- Запрос безопасен, так как использует пагинацию (LIMIT/OFFSET) для предотвращения перегрузки.
-- Параметры: $1 - ID предложения, $2 - LIMIT (лимит записей), $3 - OFFSET (смещение).
-- Фильтрация по `proposal_id` быстрая благодаря наличию композитного индекса.
SELECT * FROM proposal_additional_info
WHERE proposal_id = $1
ORDER BY info_key
LIMIT $2
OFFSET $3;

-- name: UpdateProposalAdditionalInfoValue :one
-- Обновляет только значение (info_value) для существующей записи по ее внутреннему ID.
-- Это простой и эффективный точечный запрос.
UPDATE proposal_additional_info
SET
    info_value = $2,
    updated_at = NOW()
WHERE
    id = $1
RETURNING *;

-- name: DeleteProposalAdditionalInfo :exec
-- Удаляет одну запись доп. информации по ее внутреннему ID.
-- Операция не имеет сложных побочных эффектов.
DELETE FROM proposal_additional_info
WHERE id = $1;

-- name: DeleteAllAdditionalInfoForProposal :exec
-- Удаляет ВСЕ записи дополнительной информации для указанного proposal_id.
-- Это полезная утилита для полной замены всех данных, например, при повторном импорте.
-- Запрос работает быстро, так как использует индекс по полю `proposal_id`.
DELETE FROM proposal_additional_info
WHERE proposal_id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertProposalAdditionalInfoParams struct {
    ProposalID int64          `json:"proposal_id"`
    InfoKey    string         `json:"info_key"`
    InfoValue  sql.NullString `json:"info_value"`
}

type GetProposalAdditionalInfoByKeyParams struct {
    ProposalID int64  `json:"proposal_id"`
    InfoKey    string `json:"info_key"`
}

type ListProposalAdditionalInfoByProposalIDParams struct {
    ProposalID int64 `json:"proposal_id"`
    Limit      int32 `json:"limit"`
    Offset     int32 `json:"offset"`
}

type UpdateProposalAdditionalInfoValueParams struct {
    ID        int64          `json:"id"`
    InfoValue sql.NullString `json:"info_value"`
}
*/