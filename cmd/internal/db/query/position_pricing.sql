-- name: GetPositionPricingData :many
-- Получает данные для аналитики цен по каталожной позиции.
-- Возвращает все предложения подрядчиков (исключая baseline) для данной позиции,
-- с джоинами на подрядчика, лот, тендер, единицу измерения и победителя.
-- Агрегация (min/max/avg, группировка по ед.изм.) выполняется в Go-коде.
SELECT
    t.etp_id AS tender_number,
    l.lot_key,
    c.title AS contractor_title,
    c.inn AS contractor_inn,
    pi.job_title_in_proposal AS original_title,
    pi.quantity,
    pi.unit_cost_materials,
    pi.unit_cost_works,
    pi.unit_cost_indirect_costs,
    pi.unit_cost_total,
    COALESCE(u.normalized_name, 'Не указано') AS unit_name,
    w.rank AS winner_rank,
    w.awarded_share AS winner_share,
    w.id AS winner_id,
    pi.created_at
FROM position_items pi
JOIN proposals p ON pi.proposal_id = p.id
JOIN contractors c ON p.contractor_id = c.id
JOIN lots l ON p.lot_id = l.id
JOIN tenders t ON l.tender_id = t.id
LEFT JOIN units_of_measurement u ON pi.unit_id = u.id
LEFT JOIN winners w ON p.id = w.proposal_id
WHERE
    pi.catalog_position_id = sqlc.arg(catalog_position_id)::bigint
    AND pi.is_chapter = false
    AND p.is_baseline = false
ORDER BY pi.created_at DESC;
