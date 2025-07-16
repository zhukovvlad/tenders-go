-- tenders.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- и сложных выборок для центральной сущности 'tenders'.

-- name: UpsertTender :one
-- Создает новый тендер или обновляет существующий, если тендер с таким
-- внешним ID (`etp_id`) уже существует.
-- Этот подход "создай или обнови" (Upsert) является атомарным и основным для импорта данных.
-- Возвращает полную запись созданного или обновленного тендера.
INSERT INTO tenders (
    etp_id,
    title,
    object_id,
    executor_id,
    data_prepared_on_date,
    category_id
) VALUES (
    $1, $2, $3, $4, $5, $6
)
ON CONFLICT (etp_id) DO UPDATE SET
    title = EXCLUDED.title,
    object_id = EXCLUDED.object_id,
    executor_id = EXCLUDED.executor_id,
    data_prepared_on_date = EXCLUDED.data_prepared_on_date,
    category_id = EXCLUDED.category_id,
    updated_at = NOW()
RETURNING *;

-- name: GetTenderByID :one
-- Получает один тендер по его уникальному внутреннему идентификатору (primary key).
-- Запрос очень быстрый благодаря индексу ПК.
SELECT * FROM tenders
WHERE id = $1;

-- name: GetTenderByEtpID :one
-- Получает один тендер по его уникальному внешнему идентификатору (etp_id).
-- Запрос очень быстрый, так как использует уникальный индекс.
SELECT * FROM tenders
WHERE etp_id = $1;

-- name: ListTenders :many
-- Получает обогащенный пагинированный список всех тендеров.
-- Запрос через JOIN подтягивает адрес объекта и имя исполнителя.
-- Вложенный подзапрос подсчитывает общее количество предложений для каждого тендера.
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (НА БУДУЩЕЕ) ###############
-- Сортировка `ORDER BY t.data_prepared_on_date` может быть медленной
-- на очень большом количестве тендеров, так как это поле не проиндексировано.
-- Решение: Если производительность этого списка станет важна, в будущей
-- миграции можно добавить следующий индекс:
-- CREATE INDEX idx_tenders_data_prepared_on_date ON tenders (data_prepared_on_date DESC);
-- #####################################################################
SELECT
    t.id,
    t.etp_id,
    t.title,
    t.data_prepared_on_date,
    t.category_id,
    o.address as object_address,
    e.name as executor_name,
    (
        SELECT COUNT(*)
        FROM proposals pr
        JOIN lots l_sub ON pr.lot_id = l_sub.id
        WHERE l_sub.tender_id = t.id
          AND pr.is_baseline = false
    ) as proposals_count
FROM
    tenders t
JOIN
    objects o ON t.object_id = o.id
JOIN
    executors e ON t.executor_id = e.id
ORDER BY
    t.data_prepared_on_date DESC
LIMIT $1
OFFSET $2;

-- name: UpdateTenderDetails :one
-- Обновляет детали существующего тендера по его внутреннему ID.
-- Запрос использует паттерн COALESCE, что позволяет обновлять только те поля,
-- для которых были переданы не-NULL значения, делая API гибким.
UPDATE tenders
SET
    etp_id = COALESCE(sqlc.narg(etp_id), etp_id),
    title = COALESCE(sqlc.narg(title), title),
    object_id = COALESCE(sqlc.narg(object_id), object_id),
    executor_id = COALESCE(sqlc.narg(executor_id), executor_id),
    data_prepared_on_date = COALESCE(sqlc.narg(data_prepared_on_date), data_prepared_on_date),
    category_id = COALESCE(sqlc.narg(category_id), category_id),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteTender :exec
-- Удаляет тендер по его внутреннему ID.
-- ############### ЗАМЕЧАНИЕ ПО ЛОГИКЕ (ВАЖНО!) ###############
-- ДАННАЯ ОПЕРАЦИЯ ЗАВЕРШИТСЯ С ОШИБКОЙ, если у этого тендера существует
-- хотя бы одна связанная запись в таблице `lots`.
-- Причина: для внешнего ключа `lots.tender_id` действует правило `ON DELETE RESTRICT`.
-- Это безопасное поведение, которое предотвращает появление "осиротевших" лотов.
-- Приложение должно само обрабатывать эту ошибку и запрещать удаление используемых тендеров.
-- #####################################################################
DELETE FROM tenders
WHERE id = $1;

-- name: GetTenderDetails :one
-- Получает полную, обогащенную информацию по одному тендеру для отображения на его странице.
-- Запрос эффективно собирает данные из нескольких таблиц, включая всю иерархию категорий.
-- Использование LEFT JOIN является ключевым, так как гарантирует возврат данных о тендере,
-- даже если у него не указана категория (category_id IS NULL).
SELECT
    t.id,
    t.etp_id,
    t.title,
    t.data_prepared_on_date,
    t.created_at,
    obj.title as object_title,
    obj.address as object_address,
    exc.name as executor_name,
    cat.title as category_title,
    chap.title as chapter_title,
    typ.title as type_title
FROM
    tenders t
LEFT JOIN
    objects obj ON t.object_id = obj.id
LEFT JOIN
    executors exc ON t.executor_id = exc.id
LEFT JOIN
    tender_categories cat ON t.category_id = cat.id
LEFT JOIN
    tender_chapters chap ON cat.tender_chapter_id = chap.id
LEFT JOIN
    tender_types typ ON chap.tender_type_id = typ.id
WHERE
    t.id = $1;