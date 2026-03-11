# 2026-03-11 — API просмотра групп каталога

## Контекст

После введения `kind = 'GROUP_TITLE'` (devlog 2026-03-06) операторы могут
группировать вариантные позиции под абстрактными родительскими группами через `parent_id`.
Для администраторского фронтенда необходимо два read-only эндпоинта:
список всех групп с подсчётом дочерних позиций и список дочерних позиций конкретной группы.

## Что сделано

### 1. SQL-запросы (`cmd/internal/db/query/catalog_position.sql`)

Добавлены три новых именованных запроса:

**`ListGroups :many`** — пагинированный список групп с коррелирующим подзапросом для `children_count`:
```sql
SELECT
    c1.id, c1.standard_job_title, c1.description, c1.status, c1.created_at,
    (SELECT COUNT(*) FROM catalog_positions c2 WHERE c2.parent_id = c1.id)::int AS children_count
FROM catalog_positions c1
WHERE c1.kind = 'GROUP_TITLE'
ORDER BY c1.standard_job_title
LIMIT $1 OFFSET $2;
```

**`CountGroups :one`** — общее количество групп для заголовка пагинации:
```sql
SELECT COUNT(*)::int FROM catalog_positions WHERE kind = 'GROUP_TITLE';
```

**`ListGroupChildren :many`** — дочерние позиции для конкретного `parent_id`:
```sql
SELECT id, standard_job_title, description, kind, status, created_at
FROM catalog_positions
WHERE parent_id = $1
ORDER BY standard_job_title;
```

После добавления запросов выполнена команда `make sqlc`. Сгенерированы:
`ListGroupsRow`, `ListGroupsParams`, `ListGroupChildrenRow`, `CountGroups()` —
интерфейс `Querier` обновлён автоматически, `Store` унаследовал новые методы без ручных правок.

### 2. DTO-модели (`cmd/internal/api_models/api_models.go`)

```go
type GroupSummary struct {
    ID               int64     `json:"id"`
    StandardJobTitle string    `json:"standard_job_title"`
    Description      *string   `json:"description,omitempty"`
    Status           string    `json:"status"`
    CreatedAt        time.Time `json:"created_at"`
    ChildrenCount    int       `json:"children_count"`
}

type ListGroupsResponse struct {
    Groups []GroupSummary `json:"groups"`
    Total  int            `json:"total"`
}

type ListGroupChildrenResponse struct {
    Children []CatalogPositionSummary `json:"children"`
    ParentID int64                    `json:"parent_id"`
}
```

`ListGroupChildrenResponse` переиспользует существующий `CatalogPositionSummary` — без дублирования структур.

### 3. Сервисный слой (`cmd/internal/services/catalog/catalog_service.go`)

**`ListGroups(ctx, limit, offset int32)`**:
- Валидирует `limit > 0`, `offset >= 0` (→ `apierrors.ValidationError`).
- Вызывает `CountGroups` → `ListGroups` (два отдельных запроса по аналогии с `ListPendingMerges`).
- Маппит `sql.NullString` в `*string` для поля `Description`.

**`ListGroupChildren(ctx, parentID int64)`**:
- Валидирует `parentID > 0`.
- Вызывает `ListGroupChildren` с `sql.NullInt64{Int64: parentID, Valid: true}` (соответствует типу параметра, сгенерированному sqlc).

### 4. HTTP-хендлеры (`cmd/internal/server/handlers_rag.go`)

**`ListGroupsHandler`** — `GET /api/v1/admin/catalog/groups`:
- Парсит `limit` (default `50`) и `offset` (default `0`) через `c.DefaultQuery` + `strconv.Atoi`.
- Возвращает `ListGroupsResponse` с paginated-списком и `total`.

**`ListGroupChildrenHandler`** — `GET /api/v1/admin/catalog/groups/:id/children`:
- Парсит `:id` через `c.Param` + `strconv.ParseInt`.
- Валидирует `id > 0` на уровне хендлера.

### 5. Маршруты (`cmd/internal/server/server.go`)

```go
admin.GET("/catalog/groups", server.ListGroupsHandler)
admin.GET("/catalog/groups/:id/children", server.ListGroupChildrenHandler)
```

Оба маршрута размещены внутри группы `admin` — JWT-аутентификация и `RequireRole("admin")`
применяются автоматически через существующий middleware.

## Архитектурные решения

| Решение | Обоснование |
|---------|-------------|
| `CountGroups` как отдельный запрос | Соответствует паттерну `CountPendingMerges` — нужен `Total` для UI-пагинации |
| Переиспользование `CatalogPositionSummary` | Дети имеют те же поля, что уже описаны в DTO; дублирование не нужно |
| `sql.NullInt64` для `parent_id` | sqlc генерирует именно этот тип по схеме БД; передаём `{Int64: id, Valid: true}` |
| Readonly — нет транзакций | Оба эндпоинта — чтение; `ExecTx` не требуется |

## Затронутые файлы

- `cmd/internal/db/query/catalog_position.sql` — 3 новых запроса
- `cmd/internal/db/sqlc/catalog_position.sql.go` — перегенерирован (`make sqlc`)
- `cmd/internal/db/sqlc/querier.go` — перегенерирован
- `cmd/internal/db/sqlc/mock_querier.go` — перегенерирован (`mockgen`)
- `cmd/internal/db/sqlc/mock_store.go` — перегенерирован (`mockgen`)
- `cmd/internal/api_models/api_models.go` — 3 новых DTO
- `cmd/internal/services/catalog/catalog_service.go` — 2 новых метода
- `cmd/internal/server/handlers_rag.go` — 2 новых хендлера
- `cmd/internal/server/server.go` — 2 новых маршрута

## Проверка

```bash
make sqlc   # OK — сгенерированы ListGroupsRow, CountGroups, ListGroupChildrenRow
go build ./cmd/...  # OK — нет ошибок компиляции
```

---

# 2026-03-11 — Ungroup: исключение позиции из группы

## Контекст

Оператор должен иметь возможность убрать вариантную позицию из группы. При этом:
1. `parent_id` позиции сбрасывается в `NULL`.
2. `status` возвращается в `'pending_indexing'` — Python NLP-воркер переиндексирует позицию.
3. Все существующие `suggested_merges` с `status = 'GROUPED'` для этой позиции удаляются —
   иначе Upsert-логика воркера видит уже существующую GROUPED-запись и молча оставляет её,
   не создавая новых PENDING-предложений.

Оба действия обёрнуты в одну транзакцию: либо оба выполнились, либо ни одно.

## Что сделано

### 1. SQL-запросы

**`catalog_position.sql` — `UngroupPosition :one`:**
```sql
UPDATE catalog_positions
SET parent_id = NULL,
    status = 'pending_indexing',
    updated_at = NOW()
WHERE id = $1
  AND parent_id IS NOT NULL
RETURNING *;
```
GUARD clause `AND parent_id IS NOT NULL` возвращает `sql.ErrNoRows` если позиция
не найдена или уже не состоит в группе — сервис отличает этот случай от ошибки БД.

**`suggested_merges.sql` — `DeleteGroupedMergesForPosition :exec`:**
```sql
DELETE FROM suggested_merges
WHERE status = 'GROUPED'
  AND (main_position_id = $1 OR duplicate_position_id = $1);
```
Удаляет все стороны слияния (и как главная, и как дубликат-позиция).

После добавления запросов выполнена команда `make sqlc`. Сгенерированы:
`UngroupPosition(ctx, int64) (CatalogPosition, error)` и
`DeleteGroupedMergesForPosition(ctx, int64) error`. Интерфейс `Querier` обновлён автоматически.

### 2. Сервисный слой (`catalog_service.go`)

**`UngroupPosition(ctx, positionID int64, executedBy string) error`:**
- Валидирует `positionID > 0` и непустой `executedBy` (после `TrimSpace`).
- Открывает транзакцию `ExecTx`.
- Вызывает `q.UngroupPosition`: `sql.ErrNoRows` → `apierrors.ValidationError("позиция не найдена или не состоит в группе")`.
- Вызывает `q.DeleteGroupedMergesForPosition` в той же транзакции.
- Если любой шаг падает — транзакция откатывается: позиция остаётся в группе, GROUPED-записи не удаляются.

### 3. HTTP-хендлер и маршрут

**`UngroupPositionHandler`** — `POST /api/v1/admin/catalog/positions/:id/ungroup`:
- Парсит `:id` через `strconv.ParseInt`, отклоняет `<= 0` (400).
- Извлекает `user_id` из JWT-контекста (уже установлен middleware `RequireRole("admin")`).
- Маппинг ошибок: `ValidationError` → 400, `NotFoundError` → 404, остальное → 500.
- При успехе: `{"status": "ungrouped", "position_id": <id>}`.

```go
admin.POST("/catalog/positions/:id/ungroup", server.UngroupPositionHandler)
```

## Архитектурные решения

| Решение | Обоснование |
|---------|-------------|
| Транзакция для двух операций | `UngroupPosition` + `DeleteGroupedMergesForPosition` атомарны: нельзя разгруппировать позицию и оставить GROUPED-записи |
| GUARD `AND parent_id IS NOT NULL` в SQL | Запрос идемпотентен: повторный вызов на уже разгруппированной позиции возвращает `sql.ErrNoRows`, обрабатываемый как `ValidationError` |
| `executedBy` логируется, но не пишется в БД | `catalog_positions` не имеет аудит-колонки `updated_by`; логирование достаточно для трассировки |
| `POST`, не `DELETE` | Семантика операции — "действие", а не удаление ресурса; согласуется с `/ungroup` и другими action-эндпоинтами (`/execute`, `/group`) |

## Затронутые файлы

- `cmd/internal/db/query/catalog_position.sql` — запрос `UngroupPosition`
- `cmd/internal/db/query/suggested_merges.sql` — запрос `DeleteGroupedMergesForPosition`
- `cmd/internal/db/sqlc/catalog_position.sql.go` — перегенерирован
- `cmd/internal/db/sqlc/suggested_merges.sql.go` — перегенерирован
- `cmd/internal/db/sqlc/querier.go` — перегенерирован
- `cmd/internal/db/sqlc/mock_querier.go` — перегенерирован
- `cmd/internal/db/sqlc/mock_store.go` — перегенерирован
- `cmd/internal/services/catalog/catalog_service.go` — метод `UngroupPosition`
- `cmd/internal/server/handlers_rag.go` — хендлер `UngroupPositionHandler`
- `cmd/internal/server/server.go` — маршрут `POST /catalog/positions/:id/ungroup`

## Проверка

```bash
make sqlc        # OK — сгенерированы UngroupPosition, DeleteGroupedMergesForPosition
go build ./cmd/... # OK — нет ошибок компиляции
```
