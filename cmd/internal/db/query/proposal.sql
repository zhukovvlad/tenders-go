-- proposals.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- и сложных аналитических выборок для таблицы 'proposals'.

-- name: UpsertProposal :one
-- Создает новое предложение или обновляет существующее, если предложение
-- с уникальной парой (lot_id, contractor_id) уже существует (Upsert).
-- Это основной метод для импорта данных о предложениях.
-- Возвращает полную запись созданного или обновленного предложения.
INSERT INTO proposals (
    lot_id,
    contractor_id,
    is_baseline,
    contractor_coordinate,
    contractor_width,
    contractor_height
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (lot_id, contractor_id) DO UPDATE SET
    is_baseline = EXCLUDED.is_baseline,
    contractor_coordinate = EXCLUDED.contractor_coordinate,
    contractor_width = EXCLUDED.contractor_width,
    contractor_height = EXCLUDED.contractor_height,
    updated_at = NOW()
RETURNING *;

-- name: GetProposalByID :one
-- Получает одно предложение по его уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM proposals
WHERE id = $1;

-- name: GetProposalByLotAndContractor :one
-- Получает одно предложение по его "бизнес-ключу" - комбинации ID лота и ID подрядчика.
-- Запрос очень быстрый, так как использует уникальный композитный индекс.
SELECT * FROM proposals
WHERE lot_id = $1 AND contractor_id = $2;

-- name: GetBaselineProposalForLot :one
-- Получает базовое ("эталонное") предложение для указанного lot_id.
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (НА БУДУЩЕЕ) ###############
-- ДАННЫЙ ЗАПРОС МОЖЕТ БЫТЬ НЕЭФФЕКТИВНЫМ на больших объемах данных.
-- Причина: отсутствует специализированный индекс для условия `WHERE lot_id = ? AND is_baseline = TRUE`.
-- Решение: Когда производительность станет важна, создать новую миграцию и добавить в нее индекс:
-- CREATE INDEX idx_proposals_lot_id_is_baseline ON proposals (lot_id, is_baseline);
-- #####################################################################
SELECT * FROM proposals
WHERE lot_id = $1 AND is_baseline = TRUE;

-- name: ListProposalsByLotID :many
-- Получает пагинированный список всех предложений от подрядчиков (is_baseline = FALSE) для одного лота.
-- Запрос безопасен и быстр, использует пагинацию и индекс по lot_id.
SELECT * FROM proposals
WHERE lot_id = $1 AND is_baseline = FALSE
ORDER BY id
LIMIT $2
OFFSET $3;

-- name: ListAllProposalsForLot :many
-- Получает пагинированный список ВСЕХ предложений (включая базовое) для одного лота.
-- Сортировка `is_baseline DESC` гарантирует, что базовое предложение будет первым в списке.
-- Запрос безопасен и быстр.
SELECT * FROM proposals
WHERE lot_id = $1
ORDER BY is_baseline DESC, id
LIMIT $2
OFFSET $3;

-- name: UpdateProposalDetails :one
-- Обновляет детали существующего предложения по его ID.
-- Запрос использует паттерн COALESCE, что позволяет обновлять только те поля,
-- для которых были переданы не-NULL значения, делая API гибким.
UPDATE proposals
SET
    lot_id = COALESCE(sqlc.narg(lot_id), lot_id),
    contractor_id = COALESCE(sqlc.narg(contractor_id), contractor_id),
    is_baseline = COALESCE(sqlc.narg(is_baseline), is_baseline),
    contractor_coordinate = COALESCE(sqlc.narg(contractor_coordinate), contractor_coordinate),
    contractor_width = COALESCE(sqlc.narg(contractor_width), contractor_width),
    contractor_height = COALESCE(sqlc.narg(contractor_height), contractor_height),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteProposal :exec
-- Удаляет предложение по его внутреннему ID.
-- ############### ЗАМЕЧАНИЕ ПО ЛОГИКЕ (ВАЖНО!) ###############
-- ДАННАЯ ОПЕРАЦИЯ ЗАВЕРШИТСЯ С ОШИБКОЙ, если на это предложение ссылаются
-- записи из других таблиц, например: `winners`, `position_items`, `proposal_summary_lines`.
-- Причина: для этих связей действует правило `ON DELETE RESTRICT` (по умолчанию).
-- Это безопасное поведение, но приложение должно быть готово обработать ошибку
-- и не позволять удалять предложение, у которого есть связанные данные.
-- #####################################################################
DELETE FROM proposals
WHERE id = $1;

-- name: ListProposalsForTender :many
-- Получает полный, обогащенный список предложений для указанного тендера.
-- Включает данные о подрядчике, итоговую стоимость, статус победителя и доп. информацию в виде JSON.
-- Запрос безопасен благодаря пагинации.
-- ############### ЗАМЕЧАНИЕ ПО ПРОИЗВОДИТЕЛЬНОСТИ (НА БУДУЩЕЕ) ###############
-- Сортировка `ORDER BY is_winner DESC, total_cost ASC` по вычисляемым вложенными
-- запросами полям может быть медленной на больших объемах предложений.
-- Сейчас это приемлемо, но если возникнут проблемы, решением может быть
-- денормализация - добавление полей `winner_status` и `final_cost` в саму таблицу `proposals`.
-- #####################################################################
SELECT
    p.id as proposal_id,
    c.id as contractor_id,
    c.title as contractor_title,
    c.inn as contractor_inn,
    (SELECT total_cost FROM proposal_summary_lines psl WHERE psl.proposal_id = p.id AND psl.summary_key = 'total_cost_with_vat' LIMIT 1) as total_cost,
    (SELECT EXISTS (SELECT 1 FROM winners w WHERE w.proposal_id = p.id)) as is_winner,
    (
        SELECT jsonb_object_agg(pai.info_key, pai.info_value)
        FROM proposal_additional_info pai
        WHERE pai.proposal_id = p.id
    ) as additional_info
FROM
    proposals p
JOIN
    contractors c ON p.contractor_id = c.id
JOIN
    lots l ON p.lot_id = l.id
WHERE
    l.tender_id = $1
ORDER BY
    is_winner DESC, total_cost ASC
LIMIT $2
OFFSET $3;

-- name: ListRichProposalsForLot :many
-- Получает полный, обогащенный список предложений для указанного лота,
-- исключая baseline-предложения.
-- Примечание: безопасен благодаря пагинации и фильтрации.
SELECT
    p.id AS proposal_id,
    c.id AS contractor_id,
    c.title AS contractor_title,
    c.inn AS contractor_inn,
    (
        SELECT total_cost
        FROM proposal_summary_lines psl
        WHERE psl.proposal_id = p.id AND psl.summary_key = 'total_cost_with_vat'
        LIMIT 1
    ) AS total_cost,
    (
        SELECT EXISTS (
            SELECT 1 FROM winners w WHERE w.proposal_id = p.id
        )
    ) AS is_winner,
    COALESCE((
        SELECT jsonb_object_agg(pai.info_key, pai.info_value)
        FROM proposal_additional_info pai
        WHERE pai.proposal_id = p.id AND pai.info_key IS NOT NULL
    ), '{}'::jsonb) AS additional_info
FROM
    proposals p
JOIN
    contractors c ON p.contractor_id = c.id
WHERE
    p.lot_id = $1
    AND NOT p.is_baseline
ORDER BY
    is_winner DESC,
    total_cost ASC
LIMIT $2
OFFSET $3;

-- name: GetProposalsByLotIDs :many
-- Получает все предложения для указанных лотов одним запросом,
-- избегая проблемы N+1. Включает данные подрядчика, итоговую стоимость
-- и информацию о победителе (если есть).
-- Оптимизирован для эффективной загрузки всех связанных данных за один раз.
-- Используется совместно с пагинацией лотов: загружает предложения только
-- для текущей страницы лотов, избегая загрузки лишних данных.
SELECT
    p.id,
    p.lot_id,
    p.contractor_id,
    p.is_baseline,
    c.title AS contractor_name,
    c.inn AS contractor_inn,
    psl.total_cost,
    w.id AS winner_id,
    w.rank AS winner_rank,
    w.notes AS winner_notes,
    (w.id IS NOT NULL) AS is_winner,
    COALESCE((
        SELECT jsonb_object_agg(pai.info_key, pai.info_value)
        FROM proposal_additional_info pai
        WHERE pai.proposal_id = p.id
    ), '{}'::jsonb) AS additional_info
FROM
    proposals p
JOIN
    contractors c ON p.contractor_id = c.id
LEFT JOIN
    proposal_summary_lines psl ON psl.proposal_id = p.id AND psl.summary_key = 'total_cost_with_vat'
LEFT JOIN
    winners w ON w.proposal_id = p.id
WHERE
    p.lot_id = ANY(sqlc.arg(lot_ids)::bigint[])
ORDER BY
    p.lot_id ASC,
    CASE WHEN w.id IS NOT NULL THEN 0 ELSE 1 END,
    w.rank ASC NULLS LAST,
    psl.total_cost ASC NULLS LAST;
