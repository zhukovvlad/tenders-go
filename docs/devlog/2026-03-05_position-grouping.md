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
}

type GroupPositionsResponse struct {
    MergeID    int64  `json:"merge_id"`
    ParentID   int64  `json:"parent_id"`
    Status     string `json:"status"`      // "GROUPED"
    ResolvedAt string `json:"resolved_at"`
}
```

Ровно одно из полей `ParentID` / `NewParentTitle` должно быть задано.

### 4. Сервисный метод `GroupPositions` (`catalog_service.go`)

Логика внутри `ExecTx`:

1. `GroupMerge` — атомарно PENDING/APPROVED → GROUPED
2. Определение `finalParentID`:
   - `NewParentTitle` → `CreateParentCatalogPosition` (новый HEADER)
   - `ParentID` → `GetCatalogPositionByID` + валидация (не deprecated, не merged)
3. `SetPositionParent` для MainPositionID и DuplicatePositionID

**Ключевое отличие от `ExecuteMerge`:**
- Позиции **не** становятся deprecated
- `InvalidateRelatedActionableMerges` **не** вызывается (нет «мёртвых душ»)
- `FlattenMergeChain` **не** нужен (нет merged_into_id)

### 5. HTTP Handler `GroupPositionsHandler` (`handlers_rag.go`)

Роут: `POST /api/v1/admin/merges/:id/group`

Паттерн идентичен `ExecuteMergeHandler`:
- Parse `:id` из URL
- Strict JSON decode с `DisallowUnknownFields()`
- Extract `user_id` из JWT context
- Error dispatch: ValidationError → 400, NotFoundError → 404, остальное → 500

### 6. Регистрация роута (`server.go`)

```go
admin.POST("/merges/:id/group", server.GroupPositionsHandler)
```

Между `/merges/:id/execute` и `/merges/:id/reject`.

## Статусная модель suggested_merges (обновлённая)

```text
PENDING ──→ APPROVED ──→ EXECUTED   (слияние дубликатов)
   │            │
   │            ├──→ GROUPED        (группировка вариантов) ← NEW
   │            │
   └────────────┴──→ REJECTED       (отклонение)
```

## Тестовый план

См. обновлённый `TESTING_CHECKLIST.md`:
- Unit: GroupPositions (валидация, ExecTx, ошибки)
- Handler: GroupPositionsHandler (HTTP codes, JWT, strict JSON)
- Integration: GroupMerge, CreateParentCatalogPosition, SetPositionParent
- Integration: constraints (chk_not_self_parent, ON DELETE RESTRICT)
