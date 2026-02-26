-- system_settings.sql
-- Запросы для работы с глобальными системными настройками.

-- name: GetSystemSettingByKey :one
-- Получает одну настройку по ключу.
SELECT key, value_numeric, value_string, value_boolean, description, created_at, updated_at, updated_by
FROM system_settings
WHERE key = $1
LIMIT 1;

-- name: ListSystemSettings :many
-- Возвращает все системные настройки.
SELECT key, value_numeric, value_string, value_boolean, description, created_at, updated_at, updated_by
FROM system_settings
ORDER BY key;

-- name: UpsertSystemSettingNumeric :one
-- Создает или обновляет числовую настройку.
INSERT INTO system_settings (key, value_numeric, value_string, value_boolean, description, updated_by)
VALUES ($1, $2, NULL, NULL, $3, $4)
ON CONFLICT (key)
DO UPDATE SET
    value_numeric = EXCLUDED.value_numeric,
    value_string  = NULL,
    value_boolean = NULL,
    description   = COALESCE(EXCLUDED.description, system_settings.description),
    updated_by    = EXCLUDED.updated_by
RETURNING key, value_numeric, value_string, value_boolean, description, created_at, updated_at, updated_by;

-- name: UpsertSystemSettingString :one
-- Создает или обновляет текстовую настройку.
INSERT INTO system_settings (key, value_string, value_numeric, value_boolean, description, updated_by)
VALUES ($1, $2, NULL, NULL, $3, $4)
ON CONFLICT (key)
DO UPDATE SET
    value_string  = EXCLUDED.value_string,
    value_numeric = NULL,
    value_boolean = NULL,
    description   = COALESCE(EXCLUDED.description, system_settings.description),
    updated_by    = EXCLUDED.updated_by
RETURNING key, value_numeric, value_string, value_boolean, description, created_at, updated_at, updated_by;

-- name: UpsertSystemSettingBoolean :one
-- Создает или обновляет булеву настройку.
INSERT INTO system_settings (key, value_boolean, value_numeric, value_string, description, updated_by)
VALUES ($1, $2, NULL, NULL, $3, $4)
ON CONFLICT (key)
DO UPDATE SET
    value_boolean = EXCLUDED.value_boolean,
    value_numeric = NULL,
    value_string  = NULL,
    description   = COALESCE(EXCLUDED.description, system_settings.description),
    updated_by    = EXCLUDED.updated_by
RETURNING key, value_numeric, value_string, value_boolean, description, created_at, updated_at, updated_by;

-- name: DeleteSystemSetting :exec
-- Удаляет настройку по ключу.
DELETE FROM system_settings
WHERE key = $1;
