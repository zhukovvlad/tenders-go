# 2026-03-05 — Path Compression (сжатие цепочек слияния)

## Контекст

При слиянии каталожных позиций дубликат получает `merged_into_id`, указывающий на
мастер-позицию. Со временем образуются транзитивные цепочки:

```text
Позиция D → merged_into_id = C
Позиция C → merged_into_id = B
Позиция B → merged_into_id = A   (A — active)
```

Для разрешения `D → A` нужно пройти всю цепочку (D → C → B → A). Это:
- Усложняет JOIN-запросы (рекурсивные CTE или многократные JOIN)
- Замедляет чтение при росте глубины цепочки
- Нарушает принцип «одно обращение — один ответ» в RAG-воркфлоу

### Решение

**Path Compression на уровне записи.** При каждом слиянии все позиции, ранее указывающие
на старого мастера, перенаправляются напрямую на нового мастера. После выполнения любой
merge-операции глубина цепочки всегда ≤ 1 для вновь сливаемых позиций.
Для исторических цепочек, возникших до внедрения Path Compression, может потребоваться
одноразовая миграция.

## Что сделано

### 1. SQL: `FlattenMergeChain` (`catalog_position.sql`)

```sql
-- name: FlattenMergeChain :exec
UPDATE catalog_positions
SET
    merged_into_id = sqlc.arg(new_master_id),
    updated_at = NOW()
WHERE
    merged_into_id = sqlc.arg(old_master_id);
```

- Принимает `new_master_id` (новый мастер) и `old_master_id` (позиция, теряющая роль мастера).
- Атомарно перевешивает все `merged_into_id = old_master_id` на `new_master_id`.
- Является `:exec` (не возвращает строки) — нам не нужен результат, только side-effect.
- Параметры `sql.NullInt64` — соответствует nullable колонке `merged_into_id`.

### 2. `ExecuteMerge` — одиночное слияние (`catalog_service.go`)

Вызов `q.FlattenMergeChain` добавлен внутрь транзакции `ExecTx`:

- **Сценарий 1** (Default Merge, B → A):
  Сразу после `MergeCatalogPosition(B)` — перевешиваем все позиции, ранее влитые в B,
  напрямую на A:
  ```text
  FlattenMergeChain(NewMasterID=A, OldMasterID=B)
  ```

- **Сценарий 2** (Merge-to-New, A,B → C):
  После `SetPositionMerged` для обоих — перевешиваем тех, кто ссылался на A или B,
  напрямую на C:
  ```text
  FlattenMergeChain(NewMasterID=C, OldMasterID=A)
  FlattenMergeChain(NewMasterID=C, OldMasterID=B)
  ```

### 3. `ExecuteBatchMerge` — групповое слияние (`catalog_service.go`)

Вызов `q.FlattenMergeChain` добавлен в цикл `SetPositionMerged`, сразу после каждого
успешного слияния позиции:

- **Сценарий 1** (Default Batch, все → target):
  Для каждого `posID != target`:
  ```text
  FlattenMergeChain(NewMasterID=target, OldMasterID=posID)
  ```

- **Сценарий 2** (Batch Merge-to-New, все → C):
  Для каждого `posID`:
  ```text
  FlattenMergeChain(NewMasterID=C, OldMasterID=posID)
  ```

### 4. Кодогенерация

Выполнено `make sqlc` — сгенерированы:
- `catalog_position.sql.go`: `FlattenMergeChain`, `FlattenMergeChainParams`
- `querier.go`: метод в интерфейсе `Querier`
- `mock_querier.go`, `mock_store.go`: моки для тестирования

## Порядок операций внутри транзакции

```text
1. ExecuteMerge / ExecuteMergeBatch  (PENDING/APPROVED → EXECUTED)
2. MergeCatalogPosition / SetPositionMerged  (дубликат → deprecated)
3. FlattenMergeChain  (сжатие цепочки)   ← НОВОЕ
4. InvalidateRelatedActionableMerges  (инвалидация «мёртвых душ»)
```

Порядок (2 → 3) важен: сначала мы помечаем позицию как deprecated, затем перевешиваем
тех, кто на неё ссылался. Если сделать наоборот, `FlattenMergeChain` мог бы перевесить
позиции до того, как старый мастер получит `deprecated`, создавая окно для race condition.

## Файлы затронуты

- `cmd/internal/db/query/catalog_position.sql` — добавлен `FlattenMergeChain`
- `cmd/internal/db/sqlc/catalog_position.sql.go` — сгенерировано
- `cmd/internal/db/sqlc/querier.go` — сгенерировано
- `cmd/internal/db/sqlc/mock_querier.go` — сгенерировано
- `cmd/internal/db/sqlc/mock_store.go` — сгенерировано
- `cmd/internal/services/catalog/catalog_service.go` — вызовы в `ExecuteMerge` и `ExecuteBatchMerge`
