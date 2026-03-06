# 2026-03-05 — Position Grouping (группировка вариантов)

## Контекст

В MDM-системе каталога строительных работ уже существует механизм слияния полных
дубликатов (`ExecuteMerge`). Однако часть предложений о слиянии приходится отклонять,
потому что позиции похожи, но не идентичны — это **варианты**:

```text
"Окно ПВХ 900x1200"  →  не дубликат  ←  "Окно ПВХ 900x1500"
```

Оператор видит высокий similarity_score, но слить нельзя — это разные размеры.
Раньше единственный вариант — `REJECTED`. Теперь вводим новое действие: **Group**.

### Решение

Вместо слияния (deprecated + merged_into_id) оператор группирует варианты под
абстрактным родителем (HEADER). Позиции остаются **active** — это не дубликаты, а варианты.

```text
                    ┌─ "Окно ПВХ 900x1200" (active, parent_id=Parent)
"Окно ПВХ" (HEADER) ┤
                    └─ "Окно ПВХ 900x1500" (active, parent_id=Parent)
```

## Что сделано

### 1. Миграция 000007: `parent_id`, `parameters`, `GROUPED`

**Файл:** `cmd/internal/db/migration/000007_add_position_grouping.up.sql`

- `parent_id BIGINT NULL` — FK на `catalog_positions(id)` с `ON DELETE RESTRICT`
- `parameters JSONB NULL` — для будущего хранения параметров вариантов (размеры, материал)
- `CHECK (id <> parent_id)` — защита от самоссылки
- `idx_catalog_positions_parent_id` — B-Tree для поиска детей
- `idx_catalog_positions_parameters_gin` — GIN для фильтрации `WHERE parameters @> '{"material": "ПВХ"}'`
- `GROUPED` — новый терминальный статус в `suggested_merges`

### 2. SQL-запросы

**`suggested_merges.sql` — `GroupMerge`:**

```sql
UPDATE suggested_merges
SET status = 'GROUPED', resolved_at = NOW(), resolved_by = $1
WHERE id = $2 AND status IN ('PENDING', 'APPROVED')
RETURNING *;
```

Атомарный перевод PENDING/APPROVED → GROUPED. Аналогичен `ExecuteMerge`, но с другим
терминальным статусом.

**`catalog_position.sql` — `CreateParentCatalogPosition`:**

```sql
INSERT INTO catalog_positions (standard_job_title, kind, status)
VALUES ($1, 'HEADER', 'active')
RETURNING *;
```

Создаёт абстрактную родительскую позицию. `kind='HEADER'` — не участвует в RAG-поиске,
`status='active'` — сразу доступна (эмбеддинг не нужен, это просто группирующий узел).

**`catalog_position.sql` — `SetPositionParent`:**

```sql
UPDATE catalog_positions
SET parent_id = sqlc.arg(parent_id), updated_at = NOW()
WHERE id = sqlc.arg(position_id)
  AND merged_into_id IS NULL AND status != 'deprecated'
RETURNING *;
```

Привязывает позицию к родителю. Guard clauses: нельзя привязать deprecated или
уже merged позицию. Статус **не меняется** — позиция остаётся active.

### 3. API Models (`api_models.go`)

```go
type GroupPositionsRequest struct {
    ParentID       int64  `json:"parent_id,omitempty"`
    NewParentTitle string `json:"new_parent_title,omitempty"`
    Force          bool   `json:"force,omitempty"`
}

type GroupPositionsResponse struct {
    MergeID    int64     `json:"merge_id"`
    ParentID   int64     `json:"parent_id"`
    Status     string    `json:"status"`      // "GROUPED"
    ResolvedAt time.Time `json:"resolved_at"`
}

type GroupBatchPositionsRequest struct {
    MergeIDs       []int64 `json:"merge_ids"`
    ParentID       int64   `json:"parent_id,omitempty"`
    NewParentTitle string  `json:"new_parent_title,omitempty"`
    Force          bool    `json:"force,omitempty"`
}

type GroupBatchPositionsResponse struct {
    MergeIDs    []int64   `json:"merge_ids"`
    ParentID    int64     `json:"parent_id"`
    PositionIDs []int64   `json:"position_ids"`
    Status      string    `json:"status"`
    ResolvedAt  time.Time `json:"resolved_at"`
}

type GroupConflict struct {
    PositionID         int64  `json:"position_id"`
    PositionTitle      string `json:"position_title"`
    CurrentParentID    int64  `json:"current_parent_id"`
    CurrentParentTitle string `json:"current_parent_title"`
    SiblingsCount      int64  `json:"siblings_count"`
}
```

Ровно одно из полей `ParentID` / `NewParentTitle` должно быть задано.
`Force` — принудительно перезаписывает `parent_id` для позиций, уже входящих в другую группу.

### 4. Сервисные методы (`catalog_service.go`)

#### `resolveParentID` (хелпер, выделен из дублирующегося кода)

```go
func (s *CatalogService) resolveParentID(
    ctx context.Context, q *db.Queries,
    parentID int64, newTitle string, forbiddenIDs []int64,
) (int64, error)
```

Инкапсулирует логику определения и валидации родительской позиции:
- `newTitle != ""` → `CreateParentCatalogPosition` + обработка pq 23505
- `parentID > 0` → `GetCatalogPositionByID` + валидация (deprecated, merged, kind=HEADER, forbiddenIDs)

Используется в `GroupPositions` и `GroupBatchPositions`, устраняя ~30 строк дублирования.

#### `GroupPositions` (одиночная группировка)

Логика внутри `ExecTx`:

1. `GroupMerge` — атомарно PENDING/APPROVED → GROUPED
2. `resolveParentID` — определение и валидация `finalParentID`
3. **Проверка конфликтов** (если `!force`): `detectGroupConflicts(ctx, q, positionIDs, finalParentID)` — если позиция привязана к *другому* родителю, возвращаем `ConflictError` (409). Позиции, уже привязанные к целевому `finalParentID`, пропускаются (идемпотентность).
4. `SetPositionParent` для MainPositionID и DuplicatePositionID

#### `GroupBatchPositions` (батч-группировка)

Паттерн аналогичен `ExecuteBatchMerge`, но:
- Использует `GroupMergeBatch` (не `ExecuteMergeBatch`)
- PENDING/APPROVED → GROUPED (не EXECUTED)
- `resolveParentID` — общий хелпер для определения родителя
- Позиции остаются active: `SetPositionParent` вместо `SetPositionMerged`
- Нет `InvalidateRelatedActionableMerges`, нет `FlattenMergeChain`
- Проверка конфликтов `parent_id` (если `!force`)
- `slices.Sort(positionIDs)` для детерминированного порядка UPDATE

**Ключевое отличие от `ExecuteMerge`:**
- Позиции **не** становятся deprecated
- `InvalidateRelatedActionableMerges` **не** вызывается (нет «мёртвых душ»)
- `FlattenMergeChain` **не** нужен (нет merged_into_id)

### 5. Двухфазное подтверждение (force/conflict паттерн)

**Проблема:** позиция может уже быть в другой группе (`parent_id IS NOT NULL`).

**Решение:** запрос без `force` → проверка `detectGroupConflicts()` → 409 Conflict с деталями:

```json
{
  "error": "positions_already_grouped",
  "conflicts": [
    {
      "position_id": 42,
      "position_title": "Окно ПВХ 900×1200",
      "current_parent_id": 10,
      "current_parent_title": "Двери металлические",
      "siblings_count": 3
    }
  ],
  "message": "1 из 2 позиций уже входит в группу. Передайте force=true для переноса."
}
```

Фронт показывает диалог → оператор подтверждает → повторный запрос с `force: true` → 200 OK.

**Новый тип ошибки:** `ConflictError` в `apierrors` (Message + Conflicts interface{}) → HTTP 409.

**Хелпер:** `detectGroupConflicts(ctx, q, positionIDs, targetParentID)` — для каждой позиции проверяет
`parent_id`. Позиции, уже привязанные к `targetParentID`, не считаются конфликтами (идемпотентность).
Для реальных конфликтов загружает название родителя и количество «соседей» через `CountPositionsByParentID`.

### 6. HTTP Handlers (`handlers_rag.go`)

**`GroupPositionsHandler`** — `POST /api/v1/admin/merges/:id/group`

Обновлён: теперь обрабатывает `ConflictError` → 409 с `gin.H{"error": "...", "conflicts": [...], "message": "..."}`.
Ветка `default` возвращает generic 500 (`gin.H{"error": "internal_server_error"}`) без утечки внутренних деталей.

**`GroupBatchPositionsHandler`** — `POST /api/v1/admin/merges/group-batch` (NEW)

Паттерн идентичен `ExecuteBatchMergeHandler`. Аналогично generic 500 в default-ветке.

### 7. Регистрация роутов (`server.go`)

```go
admin.POST("/merges/group-batch", server.GroupBatchPositionsHandler)
admin.POST("/merges/:id/group", server.GroupPositionsHandler)
```

### 8. SQL-запросы (новые)

**`GroupMergeBatch`** — bulk PENDING/APPROVED → GROUPED (аналог `ExecuteMergeBatch`).

**`CountPositionsByParentID`** — `SELECT COUNT(*) WHERE parent_id = $1` для вычисления siblings_count.

### 9. Фикс: `UpsertSuggestedMerge` — защита GROUPED от сброса

`GROUPED` добавлен в список терминальных статусов в CASE-выражении `UpsertSuggestedMerge`:

```sql
status = CASE WHEN suggested_merges.status IN ('APPROVED', 'REJECTED', 'EXECUTED', 'GROUPED')
         THEN suggested_merges.status ELSE 'PENDING' END
```

Без этого фикса RAG-воркер мог случайно сбросить `GROUPED` обратно в `PENDING`.

## Статусная модель suggested_merges (обновлённая)

```text
PENDING ──→ APPROVED ──→ EXECUTED   (слияние дубликатов)
   │            │
   ├────────────┼──→ GROUPED        (группировка вариантов) ← NEW
   │            │
   └────────────┴──→ REJECTED       (отклонение)
```

## Тестовый план

См. обновлённый `TESTING_CHECKLIST.md`:
- Unit: resolveParentID (хелпер: создание GROUP_TITLE, валидация существующего, forbiddenIDs)
- Unit: GroupPositions (валидация, force/conflict, идемпотентность, ExecTx, ошибки)
- Unit: GroupBatchPositions (валидация, дубликаты merge_ids, force/conflict, идемпотентность, ExecTx)
- Unit: detectGroupConflicts (позиция с другим parent, позиция с целевым parent → skip, без parent, ошибки БД)
- Handler: GroupPositionsHandler (HTTP codes, JWT, strict JSON, 409 Conflict, generic 500)
- Handler: GroupBatchPositionsHandler (HTTP codes, JWT, strict JSON, 409 Conflict, generic 500)
- Integration: GroupMerge, GroupMergeBatch, CreateParentCatalogPosition, SetPositionParent, CountPositionsByParentID
- Integration: UpsertSuggestedMerge с GROUPED → не сбрасывает в PENDING
- Integration: constraints (chk_not_self_parent, ON DELETE RESTRICT)
