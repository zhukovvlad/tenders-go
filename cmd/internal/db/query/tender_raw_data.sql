-- name: UpsertTenderRawData :one
-- Создает новую запись с исходным JSON, если она не существует,
-- или обновляет существующую, если тендер с таким ID уже есть.
-- При обновлении также меняется поле updated_at.
INSERT INTO tender_raw_data (
  tender_id,
  raw_data,
  created_at,
  updated_at
) VALUES (
  $1, $2, now(), now()
)
ON CONFLICT (tender_id)
DO UPDATE SET
  raw_data = EXCLUDED.raw_data,
  updated_at = now()
RETURNING *;

-- name: GetTenderRawData :one
-- Получает исходные JSON-данные для указанного тендера.
-- (Этот метод все еще полезен для фоновых задач)
SELECT * FROM tender_raw_data
WHERE tender_id = $1;

-- name: DeleteTenderRawData :exec
-- Удаляет запись с исходным JSON для указанного тендера.
DELETE FROM tender_raw_data
WHERE tender_id = $1;
