-- proposal_summary_lines.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над таблицей 'proposal_summary_lines'.
-- Таблица хранит итоговые (сводные) строки для предложений подрядчиков.

-- name: UpsertProposalSummaryLine :one
-- Создает новую итоговую строку для предложения или обновляет существующую,
-- если запись с парой (proposal_id, summary_key) уже существует (Upsert).
-- Этот подход является атомарным и основным для импорта данных.
-- Возвращает полную запись созданного или обновленного объекта.
INSERT INTO proposal_summary_lines (
    proposal_id,
    summary_key,
    job_title,
    materials_cost,
    works_cost,
    indirect_costs_cost,
    total_cost
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
ON CONFLICT (proposal_id, summary_key) DO UPDATE SET
    job_title = EXCLUDED.job_title,
    materials_cost = EXCLUDED.materials_cost,
    works_cost = EXCLUDED.works_cost,
    indirect_costs_cost = EXCLUDED.indirect_costs_cost,
    total_cost = EXCLUDED.total_cost,
    updated_at = NOW()
RETURNING *;

-- name: GetProposalSummaryLineByKey :one
-- Получает одну итоговую строку по ее "бизнес-ключу" - комбинации ID предложения и ключа строки.
-- Запрос очень быстрый, так как использует уникальный композитный индекс.
SELECT * FROM proposal_summary_lines
WHERE proposal_id = $1 AND summary_key = $2;

-- name: ListProposalSummaryLinesByProposalID :many
-- Получает пагинированный список всех итоговых строк для указанного proposal_id.
-- Запрос безопасен, так как использует пагинацию (LIMIT/OFFSET) для предотвращения перегрузки.
-- Параметры: $1 - ID предложения, $2 - LIMIT (лимит записей), $3 - OFFSET (смещение).
-- Фильтрация по `proposal_id` быстрая благодаря наличию композитного индекса.
SELECT * FROM proposal_summary_lines
WHERE proposal_id = $1
ORDER BY summary_key
LIMIT $2
OFFSET $3;

-- name: UpdateProposalSummaryLineValues :one
-- Обновляет значения для существующей итоговой строки по ее ID.
-- Запрос использует паттерн COALESCE(sqlc.narg(...), ...), что позволяет обновлять
-- только те поля, для которых были переданы не-NULL значения. Это делает API гибким.
UPDATE proposal_summary_lines
SET
    job_title = COALESCE(sqlc.narg(job_title), job_title),
    materials_cost = COALESCE(sqlc.narg(materials_cost), materials_cost),
    works_cost = COALESCE(sqlc.narg(works_cost), works_cost),
    indirect_costs_cost = COALESCE(sqlc.narg(indirect_costs_cost), indirect_costs_cost),
    total_cost = COALESCE(sqlc.narg(total_cost), total_cost),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteProposalSummaryLine :exec
-- Удаляет одну итоговую строку по ее внутреннему ID.
-- Операция не имеет сложных побочных эффектов.
DELETE FROM proposal_summary_lines
WHERE id = $1;

-- name: DeleteAllSummaryLinesForProposal :exec
-- Удаляет ВСЕ итоговые строки для указанного proposal_id.
-- Это полезная утилита для полной замены всех итоговых строк, например, при повторном импорте.
-- Запрос работает быстро, так как использует индекс по полю `proposal_id`.
DELETE FROM proposal_summary_lines
WHERE proposal_id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertProposalSummaryLineParams struct {
    ProposalID        int64          `json:"proposal_id"`
    SummaryKey        string         `json:"summary_key"`
    JobTitle          string         `json:"job_title"`
    MaterialsCost     sql.NullString `json:"materials_cost"` // numeric мапится в string для точности
    WorksCost         sql.NullString `json:"works_cost"`
    IndirectCostsCost sql.NullString `json:"indirect_costs_cost"`
    TotalCost         string         `json:"total_cost"`
}

type GetProposalSummaryLineByKeyParams struct {
    ProposalID int64  `json:"proposal_id"`
    SummaryKey string `json:"summary_key"`
}

type ListProposalSummaryLinesByProposalIDParams struct {
    ProposalID int64 `json:"proposal_id"`
    Limit      int32 `json:"limit"`
    Offset     int32 `json:"offset"`
}

type UpdateProposalSummaryLineValuesParams struct {
    ID                int64          `json:"id"`
    JobTitle          sql.NullString `json:"job_title"`
    MaterialsCost     sql.NullString `json:"materials_cost"`
    WorksCost         sql.NullString `json:"works_cost"`
    IndirectCostsCost sql.NullString `json:"indirect_costs_cost"`
    TotalCost         sql.NullString `json:"total_cost"`
}
*/