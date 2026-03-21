# 2026-03-21 — Эндпоинт переименования группы каталога

## Контекст

После введения `kind = 'GROUP_TITLE'` операторы могут создавать абстрактные группы
при кластеризации позиций. Однако после создания группы не было возможности исправить
опечатку или уточнить название — оператор был вынужден удалять и пересоздавать группу вручную.

Параллельно при повторном импорте тендеров обнаружилась ошибка
`pq: could not determine data type of parameter $1` в `UpdateCatalogPositionDetails`.

## Что сделано

### 1. Исправление бага: UpdateCatalogPositionDetails (untyped NULL)

**Причина**: `lib/pq` отправляет нетипизированный NULL для `sql.NullString{Valid: false}`.
PostgreSQL не может вывести тип в выражении `$1 IS NOT NULL` без явного каста.
Ошибка проявлялась только при **повторном** импорте тендера: при первом импорте позиции
создаются, при втором — находятся в БД и `UpdateCatalogPositionDetails` пытается
выполнить UPDATE с NULL-параметрами.

**Фикс** в `cmd/internal/db/query/catalog_position.sql`:
```sql
-- До:
WHERE id = $4
  AND ($1 IS NOT NULL OR $2 IS NOT NULL OR $3 IS NOT NULL)
-- После:
WHERE id = $4
  AND ($1::text IS NOT NULL OR $2::text IS NOT NULL OR $3::bigint IS NOT NULL)
```
Явные касты `::text` / `::bigint` — стандартное решение PostgreSQL для нетипизированных параметров.
Альтернатива — миграция с `lib/pq` на `pgx`, но это выходит за рамки задачи.

После правки выполнена команда `make sqlc` для перегенерации Go-кода.

### 2. SQL-запрос RenameGroupTitle (`catalog_position.sql`)

```sql
-- name: RenameGroupTitle :one
UPDATE catalog_positions
SET
    standard_job_title = sqlc.arg(new_name),
    description        = sqlc.arg(new_name),
    status             = 'pending_indexing',
    embedding          = NULL,
    updated_at         = NOW()
WHERE
    id     = sqlc.arg(id)
    AND kind   = 'GROUP_TITLE'
    AND status != 'deprecated'
RETURNING *;
```

Ключевые решения:
- `description` обновляется вместе с `standard_job_title` — они хранят одинаковое значение для `GROUP_TITLE` (см. `CreateParentCatalogPosition`)
- `status = 'pending_indexing'` — переименование делает старый эмбеддинг невалидным; RAG-воркер переиндексирует автоматически
- `embedding = NULL` — явный сброс старого вектора, чтобы не матчить по устаревшему эмбеддингу
- `AND status != 'deprecated'` — guard clause вместо SELECT-then-UPDATE; `sql.ErrNoRows` означает «не найдено или недоступно»
- `sqlc.arg()` — именованные параметры генерируют `RenameGroupTitleParams{NewName string, ID int64}` вместо позиционных `$1`/`$2`

После добавления: `make sqlc` → сгенерированы `RenameGroupTitleParams`, `func (q *Queries) RenameGroupTitle(...)`.

### 3. Сервисный слой (`catalog_service.go`)

**`RenameGroup(ctx, id int64, newName string) (db.CatalogPosition, error)`**:
- `strings.TrimSpace(newName)` — обрезка пробелов до валидации
- `id <= 0` → `ValidationError`
- `newName == ""` → `ValidationError`
- `sql.ErrNoRows` → `NotFoundError("группа %d не найдена или недоступна для переименования")`
- `pq.Error{Code: "23505"}` → `ValidationError("группа с таким названием уже существует")`
- Логирует успешное переименование через `logger.Infof`

### 4. HTTP-хендлер (`handlers_rag.go`)

**`RenameGroupHandler`** — `PATCH /api/v1/admin/catalog/groups/:id/rename`:

```go
type renameGroupRequest struct {
    NewName string `json:"new_name"`
}
```

- Парсит `:id` через `strconv.ParseInt`, валидирует `> 0`
- Декодирует JSON-тело через `json.NewDecoder` + `DisallowUnknownFields` (ошибка декодинга → 400)
- Проверяет отсутствие лишних токенов после объекта (не `io.EOF` → 400)
- Диспетчеризация ошибок: `ValidationError` → 400, `NotFoundError` → 404, остальное → 500
- При успехе возвращает `api_models.CatalogPositionSummary` с кодом 200

Хендлер размещён рядом с `UngroupPositionHandler` и `ListGroupChildrenHandler` в `handlers_rag.go`.

### 5. Маршрут (`server.go`)

```go
admin.PATCH("/catalog/groups/:id/rename", server.RenameGroupHandler)
```

Добавлен в группу `admin` рядом с существующими маршрутами для работы с группами.
JWT-аутентификация, `RequireRole("admin")` и CSRF-middleware применяются автоматически.

## Архитектурные решения

| Решение | Обоснование |
|---------|-------------|
| `PATCH .../rename` вместо `PUT .../` | Частичное обновление одного поля; семантика PATCH точнее |
| Guard clause в SQL | Защита от race condition без отдельного SELECT; `ErrNoRows` — единственный код ошибки |
| `status = 'pending_indexing'` в SQL | Переиндексация — обязанность сервера, не клиента; атомарно с UPDATE |
| `embedding = NULL` | Немедленный сброс невалидного вектора; воркер заполнит при следующем проходе |
| `NotFoundError` при `ErrNoRows` | Объединяет «группа не найдена» и «группа deprecated» в одну ошибку без доп. SELECT |
| Возврат `db.CatalogPosition` | Клиент получает актуальное состояние без дополнительного GET; соответствует паттерну проекта |

## Затронутые файлы

- `cmd/internal/db/query/catalog_position.sql` — фикс `::text`/`::bigint` кастов + новый запрос `RenameGroupTitle`
- `cmd/internal/db/sqlc/catalog_position.sql.go` — перегенерирован (`make sqlc`)
- `cmd/internal/db/sqlc/querier.go` — перегенерирован
- `cmd/internal/db/sqlc/mock_querier.go` — перегенерирован (`mockgen`)
- `cmd/internal/db/sqlc/mock_store.go` — перегенерирован (`mockgen`)
- `cmd/internal/services/catalog/catalog_service.go` — новый метод `RenameGroup`
- `cmd/internal/server/handlers_rag.go` — новый хендлер `RenameGroupHandler` + `renameGroupRequest`
- `cmd/internal/server/server.go` — новый маршрут `PATCH /catalog/groups/:id/rename`
