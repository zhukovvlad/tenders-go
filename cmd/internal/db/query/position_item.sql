-- position_items.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- над таблицей 'position_items', которая хранит детализированные строки из предложений подрядчиков.
-- Запросы написаны с учетом производительности и безопасности для использования с sqlc.

-- name: UpsertPositionItem :one
-- Создает новую позицию в предложении или обновляет существующую, если позиция
-- с уникальной парой (proposal_id, position_key_in_proposal) уже существует.
-- Этот подход "создай или обнови" (Upsert) является атомарным и основным для импорта данных.
--
-- ############### ЗАМЕЧАНИЕ ПО ПОДДЕРЖКЕ КОДА (ВАЖНО!) ###############
-- ДАННЫЙ ЗАПРОС ЯВЛЯЕТСЯ "ХРУПКИМ" ИЗ-ЗА БОЛЬШОГО КОЛИЧЕСТВА ПОЛЕЙ.
-- Причина: при добавлении новой колонки в таблицу `position_items` необходимо
-- не забыть добавить ее в ДВА места в этом запросе:
-- 1. В список колонок для INSERT.
-- 2. В список присваиваний `SET` в секции `ON CONFLICT ... DO UPDATE`.
-- Пропуск одного из этих шагов приведет к тому, что запрос будет работать,
-- но данные будут обновляться некорректно. Требует повышенного внимания при доработках.
-- #####################################################################
INSERT INTO position_items (
    proposal_id,
    catalog_position_id,
    position_key_in_proposal,
    comment_organazier,
    comment_contractor,
    item_number_in_proposal,
    chapter_number_in_proposal,
    job_title_in_proposal,
    unit_id,
    quantity,
    suggested_quantity,
    total_cost_for_organizer_quantity,
    unit_cost_materials,
    unit_cost_works,
    unit_cost_indirect_costs,
    unit_cost_total,
    total_cost_materials,
    total_cost_works,
    total_cost_indirect_costs,
    total_cost_total,
    deviation_from_baseline_cost,
    is_chapter,
    chapter_ref_in_proposal
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23
)
ON CONFLICT (proposal_id, position_key_in_proposal) DO UPDATE SET
    catalog_position_id = EXCLUDED.catalog_position_id,
    comment_organazier = EXCLUDED.comment_organazier,
    comment_contractor = EXCLUDED.comment_contractor,
    item_number_in_proposal = EXCLUDED.item_number_in_proposal,
    chapter_number_in_proposal = EXCLUDED.chapter_number_in_proposal,
    job_title_in_proposal = EXCLUDED.job_title_in_proposal,
    unit_id = EXCLUDED.unit_id,
    quantity = EXCLUDED.quantity,
    suggested_quantity = EXCLUDED.suggested_quantity,
    total_cost_for_organizer_quantity = EXCLUDED.total_cost_for_organizer_quantity,
    unit_cost_materials = EXCLUDED.unit_cost_materials,
    unit_cost_works = EXCLUDED.unit_cost_works,
    unit_cost_indirect_costs = EXCLUDED.unit_cost_indirect_costs,
    unit_cost_total = EXCLUDED.unit_cost_total,
    total_cost_materials = EXCLUDED.total_cost_materials,
    total_cost_works = EXCLUDED.total_cost_works,
    total_cost_indirect_costs = EXCLUDED.total_cost_indirect_costs,
    total_cost_total = EXCLUDED.total_cost_total,
    deviation_from_baseline_cost = EXCLUDED.deviation_from_baseline_cost,
    is_chapter = EXCLUDED.is_chapter,
    chapter_ref_in_proposal = EXCLUDED.chapter_ref_in_proposal,
    updated_at = NOW()
RETURNING *;

-- name: GetPositionItemByID :one
-- Получает одну позицию предложения по ее уникальному внутреннему идентификатору (primary key).
-- Это основной и самый быстрый способ точечного получения записи.
SELECT * FROM position_items
WHERE id = $1;

-- name: GetPositionItemByProposalAndKey :one
-- Получает одну позицию предложения по ее "бизнес-ключу" - комбинации ID предложения и ключа позиции.
-- Запрос очень быстрый, так как использует уникальный композитный индекс.
SELECT * FROM position_items
WHERE proposal_id = $1 AND position_key_in_proposal = $2;

-- name: ListPositionItemsByProposalID :many
-- Получает пагинированный список всех позиций для указанного предложения (proposal_id).
-- Запрос безопасен, так как использует пагинацию (LIMIT/OFFSET) для предотвращения перегрузки.
-- Параметры: $1 - ID предложения, $2 - LIMIT (лимит записей), $3 - OFFSET (смещение).
-- Фильтрация по `proposal_id` быстрая благодаря наличию индекса `idx_position_items_proposal_id`.
SELECT * FROM position_items
WHERE proposal_id = $1
ORDER BY position_key_in_proposal -- Сортировка по ключу для консистентного порядка
LIMIT $2
OFFSET $3;

-- name: DeletePositionItem :exec
-- Удаляет одну позицию предложения по ее внутреннему ID.
-- Операция не имеет сложных побочных эффектов.
DELETE FROM position_items
WHERE id = $1;

-- name: DeleteAllPositionItemsForProposal :exec
-- Удаляет ВСЕ позиции для указанного предложения (proposal_id).
-- Это полезная утилита для полной замены всех позиций в смете, например, при повторном импорте.
-- Запрос работает быстро, так как использует индекс по полю `proposal_id`.
DELETE FROM position_items
WHERE proposal_id = $1;

/*
Для информации, структура параметров для UpsertPositionItem будет довольно большой:
type UpsertPositionItemParams struct {
    ProposalID                    int64           `json:"proposal_id"`
    CatalogPositionID             int64           `json:"catalog_position_id"`
    PositionKeyInProposal         string          `json:"position_key_in_proposal"`
    CommentOrganazier             sql.NullString  `json:"comment_organazier"`
    CommentContractor             sql.NullString  `json:"comment_contractor"`
    ItemNumberInProposal          sql.NullString  `json:"item_number_in_proposal"`
    ChapterNumberInProposal       sql.NullString  `json:"chapter_number_in_proposal"`
    JobTitleInProposal            string          `json:"job_title_in_proposal"`
    UnitID                        sql.NullInt64   `json:"unit_id"`
    Quantity                      sql.NullString  `json:"quantity"` // numeric мапится в string для точности
    SuggestedQuantity             sql.NullString  `json:"suggested_quantity"`
    TotalCostForOrganizerQuantity sql.NullString  `json:"total_cost_for_organizer_quantity"`
    UnitCostMaterials             sql.NullString  `json:"unit_cost_materials"`
    UnitCostWorks                 sql.NullString  `json:"unit_cost_works"`
    UnitCostIndirectCosts         sql.NullString  `json:"unit_cost_indirect_costs"`
    UnitCostTotal                 sql.NullString  `json:"unit_cost_total"`
    TotalCostMaterials            sql.NullString  `json:"total_cost_materials"`
    TotalCostWorks                sql.NullString  `json:"total_cost_works"`
    TotalCostIndirectCosts        sql.NullString  `json:"total_cost_indirect_costs"`
    TotalCostTotal                sql.NullString  `json:"total_cost_total"`
    DeviationFromBaselineCost     sql.NullString  `json:"deviation_from_baseline_cost"`
    IsChapter                     bool            `json:"is_chapter"`
    ChapterRefInProposal          sql.NullString  `json:"chapter_ref_in_proposal"`
}
Примечание: поля типа numeric в PostgreSQL рекомендуется мапить в string в Go при использовании
стандартного `database/sql` для избежания проблем с точностью с плавающей запятой (float64).
Библиотеки вроде `pgx` могут предложить более продвинутые типы для работы с `numeric`.
*/