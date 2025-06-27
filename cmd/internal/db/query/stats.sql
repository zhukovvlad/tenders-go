-- file: cmd/internal/db/query/stats.sql

-- name: GetTendersCount :one
SELECT count(*) FROM tenders;