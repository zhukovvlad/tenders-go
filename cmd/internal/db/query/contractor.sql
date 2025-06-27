-- contractor.sql

-- name: CreateContractor :one
-- Создает нового подрядчика.
-- inn должен быть уникальным.
-- created_at и updated_at будут установлены по умолчанию (DEFAULT now()).
INSERT INTO contractors (
    title,
    inn,
    address,
    accreditation
) VALUES (
    $1, $2, $3, $4
)
RETURNING *;

-- name: GetContractorByID :one
-- Получает подрядчика по его ID.
SELECT * FROM contractors
WHERE id = $1;

-- name: GetContractorByINN :one
-- Получает подрядчика по его ИНН (inn), который уникален.
SELECT * FROM contractors
WHERE inn = $1;

-- name: ListContractors :many
-- Получает список всех подрядчиков с пагинацией.
SELECT * FROM contractors
ORDER BY title -- или id, или created_at
LIMIT $1
OFFSET $2;

-- name: UpdateContractor :one
-- Обновляет существующего подрядчика. Поля обновляются только если передано НЕ NULL значение.
UPDATE contractors
SET
    title = COALESCE(sqlc.narg(title), title),
    inn = COALESCE(sqlc.narg(inn), inn),
    address = COALESCE(sqlc.narg(address), address),
    accreditation = COALESCE(sqlc.narg(accreditation), accreditation),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteContractor :exec
-- Удаляет подрядчика по ID.
DELETE FROM contractors
WHERE id = $1;

-- name: SearchContractorsByTitle :many
-- Ищет подрядчиков по частичному совпадению наименования (title), без учета регистра.
SELECT * FROM contractors
WHERE title ILIKE '%' || sqlc.arg(title_query)::text || '%'
ORDER BY title
LIMIT sqlc.arg(page_limit)::int
OFFSET sqlc.arg(page_offset)::int;

/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type CreateContractorParams struct {
    Title         string `json:"title"`
    Inn           string `json:"inn"`
    Address       string `json:"address"`
    Accreditation string `json:"accreditation"`
}

type UpdateContractorParams struct {
    ID            int64  `json:"id"`
    Title         string `json:"title"`
    Inn           string `json:"inn"`
    Address       string `json:"address"`
    Accreditation string `json:"accreditation"`
}

type SearchContractorsByTitleParams struct {
    Column1 sql.NullString `json:"column_1"` // Имя параметра будет зависеть от того, как вы назовете его в запросе, например, sqlc.arg(search_query)
    Limit   int32          `json:"limit"`
    Offset  int32          `json:"offset"`
}
// Для SearchContractorsByTitleParams: если использовать sqlc.arg(title_query), то будет TitleQuery string
// Я использовал позиционный параметр $1, sqlc сгенерирует для него имя.
// Чтобы было более явное имя, можно написать так:
// WHERE title ILIKE '%' || sqlc.arg(title_query)::text || '%'
// Тогда в структуре будет поле TitleQuery.
// В данном случае, для $1 будет сгенерировано что-то вроде Column1.
// Давайте исправим SearchContractorsByTitle для лучшей читаемости генерируемого кода:
*/