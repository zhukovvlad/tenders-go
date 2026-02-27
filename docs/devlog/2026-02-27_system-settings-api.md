# 2026-02-27 — System Settings API + побочный эффект dedup_distance_threshold

## Контекст

В [предыдущей итерации](2026-02-26_system-settings-and-go-tarball.md) была создана таблица
`system_settings` и SQLC-запросы для CRUD. Оставалось реализовать Go API:
сервисный слой, HTTP-хендлеры и побочный эффект при изменении порога дедупликации.

## Что сделано

### 1. SQL: `DeleteOutdatedPendingMerges` (`suggested_merges.sql`)

Добавлен запрос для очистки устаревших PENDING merge-заявок:

```sql
DELETE FROM suggested_merges
WHERE status = 'PENDING'
  AND similarity_score < (1.0 - $1::float8);
```

Формула: если порог distance уменьшился (стал строже), то `(1.0 - distance)` растёт,
и заявки с `similarity_score` ниже нового порога удаляются.

Код сгенерирован через `make sqlc` → `DeleteOutdatedPendingMerges(ctx, distanceThreshold float64)`.

### 2. API Models (`api_models.go`)

Добавлены DTO для работы с настройками:

| Структура | Назначение |
|-----------|-----------|
| `UpdateSystemSettingRequest` | Запрос на обновление настройки (key + ровно одно значение) |
| `SystemSettingResponse` | Ответ с данными настройки (all value types + аудит) |

`UpdateSystemSettingRequest` поддерживает три типа значений:
`ValueNumeric *float64`, `ValueString *string`, `ValueBoolean *bool`.

### 3. Service Layer (`settings/settings_service.go`)

Создан `SettingsService` с методами:

| Метод | Описание |
|-------|----------|
| `UpdateSetting` | Upsert + побочный эффект при `dedup_distance_threshold` |
| `GetSetting` | Получение по ключу |
| `ListSettings` | Список всех настроек |

**Побочный эффект** в `UpdateSetting`:
1. Валидация: ровно одно из value-полей задано.
2. Upsert через типизированный SQLC-запрос (`UpsertSystemSettingNumeric` / `String` / `Boolean`).
3. Если `key == "dedup_distance_threshold"` и `ValueNumeric != nil` →
   вызов `DeleteOutdatedPendingMerges(ctx, *ValueNumeric)`.
4. Если cleanup падает — возвращаем ошибку (настройка уже сохранена, но cleanup не прошёл).

Конвертация `db.SystemSetting` → `SystemSettingResponse` через helper `settingToResponse()`:
- `ValueNumeric` хранится как `sql.NullString` (PostgreSQL numeric) → `strconv.ParseFloat`.
- Timestamps форматируются в RFC3339.

### 4. HTTP Handlers (`handlers_admin.go`)

| Маршрут | Хендлер | Описание |
|---------|---------|----------|
| `PUT /api/v1/admin/settings` | `HandleUpdateSystemSetting` | Обновление настройки (strict JSON) |
| `GET /api/v1/admin/settings` | `HandleListSystemSettings` | Список всех настроек |
| `GET /api/v1/admin/settings/:key` | `HandleGetSystemSetting` | Получение по ключу |

`HandleUpdateSystemSetting`:
- Strict JSON decode (`DisallowUnknownFields`) — защита от опечаток в полях.
- `user_id` извлекается из JWT-контекста (как в `ExecuteMergeHandler`).
- Ошибки маршрутизируются: `ValidationError` → 400, `NotFoundError` → 404, остальное → 500.

### 5. Routing (`server.go`)

- `SettingsService` добавлен в `Server` struct и инициализируется в `NewServer()`.
- Маршруты зарегистрированы в группе `admin` (за `RequireRole("admin")` middleware).

### 6. Regeneration

- `make sqlc` — sqlc generate + mockgen → чисто, без ошибок.
- `go build ./cmd/...` — успешно.
- `go vet` — чисто.

## Файлы затронуты

- `cmd/internal/db/query/suggested_merges.sql` — добавлен `DeleteOutdatedPendingMerges`
- `cmd/internal/db/sqlc/*` — автогенерация (suggested_merges.sql.go, querier.go, mock_*.go)
- `cmd/internal/api_models/api_models.go` — добавлены `UpdateSystemSettingRequest`, `SystemSettingResponse`
- `cmd/internal/services/settings/settings_service.go` — **новый** (`SettingsService`)
- `cmd/internal/server/handlers_admin.go` — добавлены хендлеры настроек
- `cmd/internal/server/server.go` — `settingsService` в struct + маршруты + import
- `TESTING_CHECKLIST.md` — обновлён
- `docs/devlog/2026-02-27_system-settings-api.md` — этот файл
