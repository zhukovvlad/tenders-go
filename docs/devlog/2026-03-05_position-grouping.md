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

#### `GroupPositions` (одиночная группировка)

Логика внутри `ExecTx`:

1. `GroupMerge` — атомарно PENDING/APPROVED → GROUPED
2. Определение `finalParentID`:
   - `NewParentTitle` → `CreateParentCatalogPosition` (новый HEADER)
   - `ParentID` → `GetCatalogPositionByID` + валидация (не deprecated, не merged, kind=HEADER, не совпадает с группируемыми)
3. **Проверка конфликтов** (если `!force`): `detectGroupConflicts` — если позиция уже имеет `parent_id`, возвращаем `ConflictError` (409) со структурированными данными о конфликтах
4. `SetPositionParent` для MainPositionID и DuplicatePositionID

#### `GroupBatchPositions` (батч-группировка)

Паттерн аналогичен `ExecuteBatchMerge`, но:
- Использует `GroupMergeBatch` (не `ExecuteMergeBatch`)
- PENDING/APPROVED → GROUPED (не EXECUTED)
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

**Хелпер:** `detectGroupConflicts(ctx, q, positionIDs)` — для каждой позиции проверяет
`parent_id`, загружает название родителя и количество «соседей» через `CountPositionsByParentID`.

### 6. HTTP Handlers (`handlers_rag.go`)

**`GroupPositionsHandler`** — `POST /api/v1/admin/merges/:id/group`

Обновлён: теперь обрабатывает `ConflictError` → 409 с `gin.H{"error", "conflicts", "message"}`.

**`GroupBatchPositionsHandler`** — `POST /api/v1/admin/merges/group-batch` (NEW)

Паттерн идентичен `ExecuteBatchMergeHandler`.

### 7. Регистрация роутов (`server.go`)

```go
admin.POST("/merges/group-batch", server.GroupBatchPositionsHandler)
admin.POST("/merges/:id/group", server.GroupPositionsHandler)
```

### 8. SQL-запросы (новые)

**`GroupMergeBatch`** — bulk PENDING/APPROVED → GROUPED (аналог `ExecuteMergeBatch`).

**`CountPositionsByParentID`** — `SELECT COUNT(*) WHERE parent_id = $1` для вычисления siblings_count.

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
- Unit: GroupPositions (валидация, force/conflict, ExecTx, ошибки)
- Unit: GroupBatchPositions (валидация, дубликаты merge_ids, force/conflict, ExecTx)
- Unit: detectGroupConflicts (позиция с parent, без parent, ошибки БД)
- Handler: GroupPositionsHandler (HTTP codes, JWT, strict JSON, 409 Conflict)
- Handler: GroupBatchPositionsHandler (HTTP codes, JWT, strict JSON, 409 Conflict)
- Integration: GroupMerge, GroupMergeBatch, CreateParentCatalogPosition, SetPositionParent, CountPositionsByParentID
- Integration: constraints (chk_not_self_parent, ON DELETE RESTRICT)
