# 2026-02-25 — Merge-to-New: два сценария слияния дубликатов

## Контекст

Предыдущая реализация `POST /api/v1/admin/merges/:id/execute` поддерживала
только один сценарий: дубликат B вливается в мастер-позицию A. Однако на практике
оператору может потребоваться создать **новую** чистую позицию C с корректным
названием, а обе старые (A и B) пометить как deprecated.

## Что сделано

### 1. SQL-запросы (`catalog_position.sql`)

Добавлены два новых запроса:

- **`CreateSimpleCatalogPosition`** — создаёт новую позицию C с минимальными
  параметрами (`standard_job_title`, `kind='POSITION'`, `status='pending_indexing'`).
  Без UPSERT — всегда INSERT.

- **`SetPositionMerged`** — устанавливает `merged_into_id` и `status='deprecated'`.
  Упрощённая версия `MergeCatalogPosition`: не проверяет статус целевой позиции
  (т.к. C может быть в `pending_indexing`). Защита: `merged_into_id IS NULL`
  и `status != 'deprecated'`.

Комментарий к `ExecuteMerge` обновлён: `MergeCatalogPosition / SetPositionMerged`.

### 2. API-модели (`api_models.go`)

- **`ExecuteMergeRequest`** — новый DTO запроса:
  ```go
  type ExecuteMergeRequest struct {
      NewMainTitle string `json:"new_main_title,omitempty"`
  }
  ```

- **`ExecuteMergeResponse`** — расширен:
  - `ResultingPositionID int64` — ID позиции-результата (A в Сценарии 1, C в Сценарии 2)
  - `ResultingPositionStatus string` — статус итоговой позиции (`"active"` в S1, `"pending_indexing"` в S2)
  - `DeprecatedPositionIDs []int64` — все deprecated-ID: `[B]` в S1, `[A, B]` в S2
  - `Scenario string` — `"default"` или `"merge_to_new"`

### 3. Сервис (`catalog_service.go`)

`ExecuteMerge` принимает 4-й аргумент `newMainTitle string`:

- **Сценарий 1** (`newMainTitle == ""`): Default Merge.
  B → A через `MergeCatalogPosition` (без изменений).

- **Сценарий 2** (`newMainTitle != ""`): Merge-to-New.
  1. Создаёт C через `CreateSimpleCatalogPosition`
  2. A → C через `SetPositionMerged`
  3. B → C через `SetPositionMerged`

Вся логика атомарна внутри `ExecTx`. Диагностика ошибок вынесена в `diagnoseMergeFailure`.

### 4. Хендлер (`handlers_rag.go`)

`ExecuteMergeHandler` читает тело запроса через `c.GetRawData()` и проверяет
`len(body) > 0` для определения сценария. Пустой body = Сценарий 1.
Тело с `new_main_title` = Сценарий 2.

### 5. Тесты

- 9 существующих тестов обновлены: добавлен 4-й аргумент `""` (Сценарий 1).
- `TestExecuteMerge_Success` проверяет поля: `ResultingPositionID`, `ResultingPositionStatus`, `DeprecatedPositionIDs`, `Scenario`.
- 7 новых тестов для Сценария 2:
  - `TestExecuteMerge_MergeToNew_Success` — полный happy path, все поля response
  - `TestExecuteMerge_MergeToNew_DuplicateTitle` — pq 23505 → ValidationError
  - `TestExecuteMerge_MergeToNew_CreatePosition_DBError` — wrapped DB error
  - `TestExecuteMerge_MergeToNew_A_AlreadyDeprecated` — ValidationError
  - `TestExecuteMerge_MergeToNew_B_AlreadyDeprecated` — ValidationError
  - `TestExecuteMerge_MergeToNew_SetPositionMerged_DBError` — wrapped DB error
  - `TestExecuteMerge_WhitespaceTitle_FallsBackToScenario1` — trim edge case
- Все 16 ExecuteMerge-тестов проходят.

## Затронутые файлы

- `cmd/internal/db/query/catalog_position.sql` — +2 запроса (CreateSimpleCatalogPosition, SetPositionMerged)
- `cmd/internal/db/query/suggested_merges.sql` — обновлён комментарий ExecuteMerge
- `cmd/internal/db/sqlc/*` — перегенерированы (sqlc + mockgen)
- `cmd/internal/api_models/api_models.go` — +ExecuteMergeRequest, расширен ExecuteMergeResponse
- `cmd/internal/services/catalog/catalog_service.go` — два сценария + diagnoseMergeFailure
- `cmd/internal/server/handlers_rag.go` — парсинг опционального тела
- `cmd/internal/services/catalog/catalog_service_test.go` — обновлены под новую сигнатуру
- `TESTING_CHECKLIST.md` — обновлены задачи 2.2 и 5.8

---

## Batch Merge — `POST /api/v1/admin/merges/execute-batch`

### Проблема

На практике одна позиция может участвовать в нескольких merge-записях одновременно —
и как мастер, и как дубликат (граф зависимостей: 758→89→2→[59,98,12587], 13→2).
Последовательное выполнение одиночных merge-ов невозможно: после первого слияния
позиция становится deprecated и остальные merge-записи ломаются.

### Решение

Новый эндпоинт `POST /api/v1/admin/merges/execute-batch` обрабатывает весь
связанный граф merge-записей атомарно в одной транзакции:

1. **SQL** (`ExecuteMergeBatch :many`) — bulk `UPDATE suggested_merges SET status='EXECUTED'
   WHERE id = ANY(@ids) AND status IN ('PENDING','APPROVED') RETURNING *`
2. **Валидация** — дубликаты merge_ids, пустые ID, частичный отказ (не все ID обновились),
   target_position_id не в группе позиций
3. **Сценарий 1** (Default Batch): `target_position_id` задан → все остальные позиции
   deprecated через `SetPositionMerged(target)`. Опциональный `rename_title` → 
   `UpdateCatalogPositionDetails` (status → `pending_indexing`)
4. **Сценарий 2** (Batch Merge-to-New): `new_main_title` задан → `CreateSimpleCatalogPosition(C)`,
   все позиции deprecated через `SetPositionMerged(C)`
5. **Атомарность** — полный rollback при любой ошибке

### Затронутые файлы (batch)

- `cmd/internal/db/query/suggested_merges.sql` — +`ExecuteMergeBatch :many`
- `cmd/internal/db/sqlc/*` — перегенерированы
- `cmd/internal/api_models/api_models.go` — +`ExecuteBatchMergeRequest`, `ExecuteBatchMergeResponse`
- `cmd/internal/services/catalog/catalog_service.go` — +`ExecuteBatchMerge()`
- `cmd/internal/server/handlers_rag.go` — +`ExecuteBatchMergeHandler()`
- `cmd/internal/server/server.go` — маршрут (static route перед parameterized `:id`)
- `cmd/internal/services/catalog/catalog_service_test.go` — +12 unit тестов

### Тесты (batch)

12 новых тестов:
- 4 валидации входных данных (empty executedBy/merge_ids, duplicates, missing target)
- 3 Scenario 1: success, rename, target not in group
- 2 error paths: partial failure, already deprecated position
- 2 Scenario 2: success, duplicate title
- 1 DB error: ExecuteMergeBatch fails

Итого: 28 ExecuteMerge-тестов (16 single + 12 batch), все проходят.

## API контракт

```http
POST /api/v1/admin/merges/:id/execute

# Сценарий 1 — пустой body или {}
→ 200 { scenario: "default", resulting_position_id: <A> }

# Сценарий 2 — с переименованием
{ "new_main_title": "Новое название" }
→ 200 { scenario: "merge_to_new", resulting_position_id: <C> }
```

```http
POST /api/v1/admin/merges/execute-batch

# Сценарий 1 (Default Batch) — target_position_id задан
{ "merge_ids": [101, 102, 103], "target_position_id": 2, "rename_title": "Чистое имя" }
→ 200 { scenario: "default", resulting_position_id: 2, deprecated_position_ids: [59, 89, 98] }

# Сценарий 2 (Batch Merge-to-New) — new_main_title задан
{ "merge_ids": [101, 102, 103], "new_main_title": "Единая позиция" }
→ 200 { scenario: "merge_to_new", resulting_position_id: 300, deprecated_position_ids: [2, 59, 89, 98] }
```
