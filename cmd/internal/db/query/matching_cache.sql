-- matching_cache.sql
-- Запросы для работы с кэшем семантического матчинга.

-- name: GetMatchingCache :one
-- (Для Go-сервера) Быстро проверяет кэш по хешу и версии.
SELECT * FROM matching_cache
WHERE 
    job_title_hash = $1
    AND norm_version = $2;

-- name: UpsertMatchingCache :exec
-- (Для Python-воркера) Записывает результат RAG-поиска в кэш.
INSERT INTO matching_cache (
    job_title_hash,
    norm_version,
    job_title_text,
    catalog_position_id,
    expires_at
) VALUES (
    $1, $2, $3, $4, $5
)
ON CONFLICT (job_title_hash, norm_version) 
DO UPDATE SET
    catalog_position_id = EXCLUDED.catalog_position_id,
    expires_at = EXCLUDED.expires_at,
    job_title_text = EXCLUDED.job_title_text;
-- (RETURNING * удален)

-- name: RetargetMatchingCache :exec
-- (Для Go-сервера, при слиянии) Перенаправляет все кэшированные
-- записи со старого ID дубликата на новый ID.
UPDATE matching_cache
SET catalog_position_id = $1 -- $1 = main_id
WHERE catalog_position_id = $2; -- $2 = duplicate_id

-- name: ClearExpiredMatchingCache :exec
-- (Для Cron-джоба) Очищает "тухлый" кэш.
DELETE FROM matching_cache
WHERE expires_at IS NOT NULL AND expires_at < now();