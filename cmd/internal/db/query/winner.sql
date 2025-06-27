-- winners.sql

-- name: UpsertWinner :one
-- Помечает предложение как выигрышное или обновляет детали победы,
-- если для данного proposal_id уже существует запись.
-- rank, awarded_share и notes могут быть NULL.
INSERT INTO winners (
    proposal_id,
    rank,
    awarded_share,
    notes
) VALUES (
    $1, $2, $3, $4
)
ON CONFLICT (proposal_id) DO UPDATE SET
    rank = EXCLUDED.rank,
    awarded_share = EXCLUDED.awarded_share,
    notes = EXCLUDED.notes,
    updated_at = NOW()
RETURNING *;

-- name: GetWinnerByID :one
-- Получает запись о победе по ее внутреннему ID.
SELECT * FROM winners
WHERE id = $1;

-- name: GetWinnerByProposalID :one
-- Получает запись о победе по ID предложения (proposal_id).
-- Удобно, чтобы проверить, является ли конкретное предложение выигрышным.
SELECT * FROM winners
WHERE proposal_id = $1;

-- name: ListWinnersForLot :many
SELECT w.id, w.proposal_id, w.rank, w.awarded_share, w.notes, w.created_at, w.updated_at FROM winners w
JOIN proposals p ON w.proposal_id = p.id
WHERE p.lot_id = $1
ORDER BY w.rank ASC, w.created_at ASC
LIMIT $2
OFFSET $3;


-- name: UpdateWinnerDetails :one
UPDATE winners
SET
    rank = COALESCE(sqlc.narg(rank), rank),
    awarded_share = COALESCE(sqlc.narg(awarded_share), awarded_share),
    notes = COALESCE(sqlc.narg(notes), notes),
    updated_at = NOW()
WHERE
    id = sqlc.arg(id)
RETURNING *;

-- name: DeleteWinner :exec
-- Удаляет запись о победе по ID предложения (proposal_id).
-- Фактически, "отменяет" победу данного предложения.
DELETE FROM winners
WHERE proposal_id = $1;


/*
Для информации, вот какие структуры параметров sqlc может сгенерировать:

type UpsertWinnerParams struct {
    ProposalID    int64             `json:"proposal_id"`
    Rank          sql.NullInt32     `json:"rank"`        // int -> sql.NullInt32
    AwardedShare  sql.NullFloat64   `json:"awarded_share"` // numeric(5,2) -> sql.NullFloat64 (или другой, в зависимости от маппинга numeric)
    Notes         sql.NullString    `json:"notes"`       // text -> sql.NullString
}

type UpdateWinnerDetailsParams struct {
    ID            int64             `json:"id"`
    Rank          sql.NullInt32     `json:"rank"`
    AwardedShare  sql.NullFloat64   `json:"awarded_share"`
    Notes         sql.NullString    `json:"notes"`
}
*/