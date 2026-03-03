# 2026-03-03 — Инвалидация «мёртвых душ» в suggested_merges

## Контекст

После выполнения слияния позиций (Execute Merge / Execute Batch Merge) дубликат получает
статус `deprecated`. Однако в таблице `suggested_merges` могли оставаться другие
PENDING-заявки, в которых участвует эта же позиция — как `main_position_id` или
`duplicate_position_id`. Такие заявки зависали навсегда и вызывали ошибку при попытке
исполнения оператором, поскольку одна из позиций пары уже `deprecated`.

### Пример проблемы

```text
Позиции: A, B, C
suggested_merges:
  #10: A ↔ B (PENDING)   ← оператор исполняет, B → deprecated
  #11: B ↔ C (PENDING)   ← "мёртвая душа": B уже deprecated, но заявка висит
  #12: A ↔ C (PENDING)   ← тоже может быть невалидной в Merge-to-New (A deprecated)
```

После исполнения #10, заявка #11 навсегда остаётся в очереди оператора и даёт ошибку
при попытке execute.

## Что сделано

### 1. SQL: `InvalidateRelatedPendingMerges` (`suggested_merges.sql`)

Новый запрос, который атомарно отклоняет все PENDING и APPROVED заявки с участием deprecated-позиций:

```sql
-- name: InvalidateRelatedPendingMerges :exec
UPDATE suggested_merges
SET
    status = 'REJECTED',
    resolved_at = NOW(),
    resolved_by = 'system'
WHERE
    status IN ('PENDING', 'APPROVED')
    AND (
        main_position_id = ANY(@position_ids::bigint[])
        OR duplicate_position_id = ANY(@position_ids::bigint[])
    );
```

- Принимает массив `position_ids` — ID всех позиций, ставших `deprecated` в результате слияния.
- Покрывает оба статуса (`PENDING` и `APPROVED`), т.к. `ExecuteMerge` принимает
  `status IN ('PENDING', 'APPROVED')` — APPROVED-заявки с deprecated-позициями точно
  так же становятся невалидными.
- Статус `REJECTED` (а не `DELETE`) сохраняет аудит-трейл — видно, что заявка была
  автоматически отклонена системой.
- `resolved_by = 'system'` отличает автоматическую инвалидацию от ручного решения оператора.

### 2. `ExecuteMerge` — одиночное слияние (`catalog_service.go`)

Вызов `q.InvalidateRelatedPendingMerges(ctx, deprecatedPositionIDs)` добавлен внутрь
транзакции, после формирования `deprecatedPositionIDs` (оба сценария — Default Merge
и Merge-to-New):

- **Сценарий 1** (Default Merge, B → A): `deprecatedPositionIDs = [B]`
- **Сценарий 2** (Merge-to-New, A,B → C): `deprecatedPositionIDs = [A, B]`

### 3. `ExecuteBatchMerge` — групповое слияние (`catalog_service.go`)

Аналогичный вызов добавлен внутрь транзакции батч-слияния, после цикла deprecate
и перед сортировкой `deprecatedPositionIDs`.

### 4. Миграция 000006: частичные индексы (`add_merge_invalidation_indexes`)

Без индексов `InvalidateRelatedPendingMerges` делал бы seq scan по `suggested_merges`.
Добавлены два частичных индекса для обоих путей поиска:

```sql
CREATE INDEX idx_suggested_merges_main_pos_actionable
ON suggested_merges (main_position_id)
WHERE status IN ('PENDING', 'APPROVED');

CREATE INDEX idx_suggested_merges_dup_pos_actionable
ON suggested_merges (duplicate_position_id)
WHERE status IN ('PENDING', 'APPROVED');
```

Частичное условие `WHERE status IN ('PENDING', 'APPROVED')` минимизирует размер индекса —
после исполнения/отклонения записи выпадают из индекса автоматически.

### 5. FOR UPDATE — анализ и решение

Пессимистичные блокировки (`SELECT ... FOR UPDATE`) **не требуются**:

- `ExecuteMerge` SQL: `WHERE id = $2 AND status IN ('PENDING', 'APPROVED')` —
  атомарный UPDATE, второй параллельный вызов получит `sql.ErrNoRows`.
- `MergeCatalogPosition`: `WHERE dup.merged_into_id IS NULL AND dup.status != 'deprecated'` —
  аналогичная защита на уровне позиции.
- PostgreSQL обеспечивает Row-Level Locking при UPDATE внутри транзакции.
- Дополнительный `SELECT FOR UPDATE` добавил бы лишний round-trip без выигрыша.

## Файлы затронуты

- `cmd/internal/db/query/suggested_merges.sql` — добавлен `InvalidateRelatedPendingMerges`
- `cmd/internal/db/migration/000006_add_merge_invalidation_indexes.{up,down}.sql` — частичные индексы
- `cmd/internal/db/sqlc/*` — автогенерация (`make sqlc`)
- `cmd/internal/services/catalog/catalog_service.go` — вызов инвалидации в `ExecuteMerge` и `ExecuteBatchMerge`
- `docs/devlog/2026-03-03_invalidate-dead-merges.md` — этот файл
