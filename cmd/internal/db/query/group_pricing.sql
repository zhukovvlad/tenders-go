-- name: GetGroupPricingData :many
-- Получает данные для аналитики цен по группе каталожных позиций.
-- Использует WITH RECURSIVE для обхода всего дерева потомков группы.
-- Возвращает все предложения подрядчиков (исключая baseline) для позиций в дереве,
-- с джоинами на подрядчика, лот, тендер, единицу измерения и победителя.
-- Агрегация (min/max/avg, группировка по ед.изм.) выполняется в Go-коде.
WITH RECURSIVE group_tree AS (
    -- Базовый узел (запрашиваемая группа)
    SELECT id, standard_job_title
    FROM catalog_positions
    WHERE id = sqlc.arg(group_id)::bigint

    UNION ALL

    -- Рекурсивный спуск по детям
    SELECT cp.id, cp.standard_job_title
    FROM catalog_positions cp
    JOIN group_tree gt ON cp.parent_id = gt.id
)
SELECT
    t.etp_id AS tender_number,
    l.lot_key,
    c.title AS contractor_title,
    c.inn AS contractor_inn,
    pi.job_title_in_proposal AS original_title,

    -- Название конкретной позиции из справочника
    gt.standard_job_title AS child_position_title,

    pi.quantity,
    pi.unit_cost_materials,
    pi.unit_cost_works,
    pi.unit_cost_indirect_costs,
    pi.unit_cost_total,
    COALESCE(u.normalized_name, 'Не указано') AS unit_name,
    w.rank AS winner_rank,
    w.awarded_share AS winner_share,
    pi.created_at
FROM position_items pi
JOIN group_tree gt ON pi.catalog_position_id = gt.id
JOIN proposals p ON pi.proposal_id = p.id
JOIN contractors c ON p.contractor_id = c.id
JOIN lots l ON p.lot_id = l.id
JOIN tenders t ON l.tender_id = t.id
LEFT JOIN units_of_measurement u ON pi.unit_id = u.id
LEFT JOIN winners w ON p.id = w.proposal_id
WHERE
    pi.is_chapter = false
    AND p.is_baseline = false
ORDER BY pi.created_at DESC;
