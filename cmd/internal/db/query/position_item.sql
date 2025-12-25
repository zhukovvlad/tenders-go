-- position_items.sql
-- (Версия 3, исправлена ошибка sqlc)
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- над таблицей 'position_items'.

-- name: UpsertPositionItem :one
-- Создает новую позицию в предложении или обновляет существующую.
--
-- ИЗМЕНЕНИЕ v4 (RAG Workflow):
-- `catalog_position_id` ($2) теперь `NULLABLE` (тип sql.NullInt64).
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
    $1, 
    $2, -- <-- ИСПРАВЛЕНО: Просто $2. sqlc сам увидит NULLABLE.
    $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23
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
SELECT * FROM position_items
WHERE id = $1;

-- name: GetPositionItemByProposalAndKey :one
-- Получает одну позицию предложения по ее "бизнес-ключу" - комбинации ID предложения и ключа позиции.
SELECT * FROM position_items
WHERE proposal_id = $1 AND position_key_in_proposal = $2;

-- name: ListPositionItemsByProposalID :many
-- Получает пагинированный список всех позиций для указанного предложения (proposal_id).
-- (v2 - Исправлена сортировка на "человеческую" по item_number_in_proposal)
SELECT * FROM position_items
WHERE proposal_id = $1
-- ИСПРАВЛЕНО: Сортируем по "номеру" как по числу, а не по "ключу" как по строке.
-- ИСПРАВЛЕНО v3 (R-safe): Безопасная сортировка. Предотвращает ошибку, если
-- item_number_in_proposal содержит нечисловые значения.
ORDER BY
    CASE
        -- Проверяем, что строка является валидным числом (целым или десятичным)
        WHEN item_number_in_proposal ~ '^[0-9]+(\.[0-9]+)?$'
            THEN item_number_in_proposal::numeric
        ELSE NULL
    END NULLS LAST, id
LIMIT $2
OFFSET $3;

-- name: DeletePositionItem :exec
-- Удаляет одну позицию предложения по ее внутреннему ID.
DELETE FROM position_items
WHERE id = $1;

-- name: DeleteAllPositionItemsForProposal :exec
-- Удаляет ВСЕ позиции для указанного предложения (proposal_id).
DELETE FROM position_items
WHERE proposal_id = $1;

-- #####################################################################
-- НОВЫЕ ЗАПРОСЫ ДЛЯ RAG-ВОРКФЛОУ (добавлены в v4)
-- #####################################################################

-- name: RetargetPositionItems :exec
-- (Для Go-сервера / Админки) Атомарно "перевешивает" все position_items
-- со старого ID дубликата на новый ID основной (канонической) записи.
-- Используется при слиянии дубликатов.
UPDATE position_items
SET catalog_position_id = $1 -- $1 = main_id
WHERE catalog_position_id = $2; -- $2 = duplicate_id

-- name: ListOrphanPositionItems :many
-- (Для Python-воркера) Находит "осиротевшие" position_items
-- (где catalog_position_id IS NULL). Это основная "очередь" для воркера.
SELECT * FROM position_items
WHERE catalog_position_id IS NULL
LIMIT $1;

-- name: SetCatalogPositionID :exec
-- (Для Python-воркера) "Закрывает" "осиротевшую" запись,
-- установив catalog_position_id после RAG-поиска.
UPDATE position_items
SET catalog_position_id = $1 -- $1 = main_id (найденный RAG-поиском)
WHERE id = $2; -- $2 = id "осиротевшей" записи

-- name: GetUnmatchedPositions :many
-- (Версия 6: Hardened - с TRIM, защитой от циклов и ORDER BY)

-- 1. (CTE) Рекурсивно строим "дерево" разделов
WITH RECURSIVE Breadcrumbs AS (
    -- 1a. (Anchor) Находим "корневые" разделы
    SELECT
        pi.id,
        pi.proposal_id,
        pi.item_number_in_proposal,
        pi.job_title_in_proposal,
        pi.chapter_ref_in_proposal,
        pi.job_title_in_proposal AS parent_path,
        ARRAY[pi.id] AS path_ids -- (ВКЛЮЧЕНО: Защита от циклов)
    FROM 
        position_items AS pi
    JOIN proposals p ON pi.proposal_id = p.id 
    JOIN lots l ON p.lot_id = l.id
    WHERE 
        pi.is_chapter = true
        AND (pi.chapter_ref_in_proposal IS NULL OR pi.chapter_ref_in_proposal = '')
        -- (ВКЛЮЧЕНО: TRIM для надежного сравнения)
        AND TRIM(pi.job_title_in_proposal) != TRIM(l.lot_title)

    UNION ALL

    -- 1b. (Recursive) Присоединяем дочерние разделы
    SELECT
        pi.id,
        pi.proposal_id,
        pi.item_number_in_proposal,
        pi.job_title_in_proposal,
        pi.chapter_ref_in_proposal,
        b.parent_path || ' | ' || pi.job_title_in_proposal,
        b.path_ids || pi.id -- (ВКЛЮЧЕНО: Защита от циклов)
    FROM 
        position_items pi
    JOIN 
        Breadcrumbs b ON pi.proposal_id = b.proposal_id 
                    -- (ВКЛЮЧЕНО: TRIM для надежного JOIN)
                    AND TRIM(pi.chapter_ref_in_proposal) = TRIM(b.item_number_in_proposal)
    WHERE 
        pi.is_chapter = true
        -- (ВКЛЮЧЕНО: Защита от циклов)
        AND NOT (pi.id = ANY(b.path_ids))
)
-- 2. (Final Query) Находим 'NULL'-позиции
-- Добавлен JOIN с catalog_positions для получения draft_catalog_id и standard_job_title
SELECT 
    pi.id AS position_item_id,
    pi.job_title_in_proposal,
    COALESCE(b.parent_path, '') AS full_parent_path,
    cp.id AS draft_catalog_id,
    cp.standard_job_title
FROM 
    position_items AS pi
LEFT JOIN 
    Breadcrumbs AS b ON b.proposal_id = pi.proposal_id 
                     -- (ВКЛЮЧЕНО: TRIM для надежного JOIN)
                     AND TRIM(b.item_number_in_proposal) = TRIM(pi.chapter_ref_in_proposal)
LEFT JOIN
    catalog_positions AS cp ON pi.catalog_position_id = cp.id
WHERE 
    (pi.catalog_position_id IS NULL OR cp.status = 'pending_indexing')
    AND pi.is_chapter = false
ORDER BY
    pi.id -- (ВКЛЮЧЕНО: Детерминированный LIMIT)
LIMIT $1;

-- name: ListPositionsForEstimate :many
-- Полный список строк КП (позиции + главы) для страницы просмотра предложения.
-- Вытаскивает все расчетные поля (кол-ва, стоимости, отклонения), комментарии,
-- а также текстовые названия ЕИ и каталожной позиции.
-- Порядок строк сохраняется по ключу из JSON (position_key_in_proposal).
SELECT
    pi.id,
    pi.proposal_id,
    pi.catalog_position_id,
    pi.position_key_in_proposal,

    pi.item_number_in_proposal,
    pi.chapter_number_in_proposal,
    pi.chapter_ref_in_proposal,
    pi.job_title_in_proposal,
    pi.is_chapter,

    pi.comment_organazier,
    pi.comment_contractor,

    pi.unit_id,
    u.normalized_name AS unit_name,

    pi.quantity,
    pi.suggested_quantity,
    pi.total_cost_for_organizer_quantity,

    pi.unit_cost_materials,
    pi.unit_cost_works,
    pi.unit_cost_indirect_costs,
    pi.unit_cost_total,

    pi.total_cost_materials,
    pi.total_cost_works,
    pi.total_cost_indirect_costs,
    pi.total_cost_total,

    pi.deviation_from_baseline_cost,

    pi.created_at,
    pi.updated_at,

    cp.standard_job_title AS catalog_name
FROM
    position_items pi
LEFT JOIN
    units_of_measurement u ON pi.unit_id = u.id
LEFT JOIN
    catalog_positions cp ON pi.catalog_position_id = cp.id
WHERE
    pi.proposal_id = $1
ORDER BY
    CASE
        WHEN pi.position_key_in_proposal ~ '^\d+$'
            THEN pi.position_key_in_proposal::int
    END ASC,
    pi.position_key_in_proposal ASC,
    pi.id ASC;
