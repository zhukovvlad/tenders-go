-- person.sql
-- Этот файл содержит набор SQL-запросов для выполнения CRUD-операций
-- (Create, Read, Update, Delete) над таблицей 'persons'.
-- Запросы написаны с учетом производительности и безопасности для использования с sqlc.

-- name: CreatePerson :one
-- Создает новую запись для контактного лица, связанного с подрядчиком.
-- Поле email может быть NULL.
-- Поля `created_at` и `updated_at` устанавливаются автоматически значением по умолчанию (now()).
-- Возвращает полную запись созданного контактного лица.
INSERT INTO persons (
    name,
    phone,
    email,
    contractor_id
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetPersonByID :one
-- Получает одну запись контактного лица по его уникальному внутреннему идентификатору (primary key).
-- Это основной и самый быстрый способ точечного получения записи.
SELECT * FROM persons
WHERE id = $1;

-- name: ListPersonsByContractor :many
-- Получает пагинированный список всех контактных лиц для указанного подрядчика (contractor_id).
-- ############### РЕКОМЕНДАЦИЯ ПО ОПТИМИЗАЦИИ (ВАЖНО!) ###############
-- ДАННЫЙ ЗАПРОС БУДЕТ РАБОТАТЬ МЕДЛЕННО на больших объемах данных.
-- Причина: отсутствует индекс по колонке `contractor_id`. PostgreSQL не создает
-- его автоматически для внешних ключей.
-- Решение: Создать новую миграцию и добавить в нее следующий индекс:
-- CREATE INDEX idx_persons_contractor_id ON persons (contractor_id);
--
-- Для еще лучшей производительности (с учетом сортировки ORDER BY name):
-- CREATE INDEX idx_persons_contractor_id_name ON persons (contractor_id, name);
-- #####################################################################
SELECT * FROM persons
WHERE contractor_id = $1
ORDER BY name
LIMIT $2
OFFSET $3;

-- name: ListAllPersons :many
-- Получает пагинированный список абсолютно всех контактных лиц в системе.
-- Может использоваться для административных панелей.
-- Примечание: сортировка по неиндексированному полю `name` может быть медленной на больших данных.
SELECT * FROM persons
ORDER BY name
LIMIT $1
OFFSET $2;

-- name: UpdatePerson :one
-- Обновляет существующее контактное лицо.
-- Запрос использует паттерн COALESCE(sqlc.narg(...), ...), что позволяет обновлять
-- только те поля, для которых были переданы не-NULL значения. Это делает API гибким.
-- Комментарий по contractor_id оставлен как пример контроля бизнес-логики.
UPDATE persons
SET
    name = COALESCE(sqlc.narg(name), name),
    phone = COALESCE(sqlc.narg(phone), phone),
    email = COALESCE(sqlc.narg(email), email),
    -- contractor_id = COALESCE(sqlc.narg(contractor_id), contractor_id), -- Для смены "владельца"
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeletePerson :exec
-- Удаляет контактное лицо по ID.
-- Эта операция не имеет сложных побочных эффектов (каскадного удаления),
-- так как на таблицу `persons` не ссылаются другие таблицы.
DELETE FROM persons
WHERE id = $1;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreatePersonParams struct {
    Name         string         `json:"name"`
    Phone        string         `json:"phone"`
    Email        sql.NullString `json:"email"`
    ContractorID int64          `json:"contractor_id"`
}

type ListPersonsByContractorParams struct {
    ContractorID int64 `json:"contractor_id"`
    Limit        int32 `json:"limit"`
    Offset       int32 `json:"offset"`
}

type UpdatePersonParams struct {
    ID    int64          `json:"id"`
    Name  sql.NullString `json:"name"`
    Phone sql.NullString `json:"phone"`
    Email sql.NullString `json:"email"`
}
*/