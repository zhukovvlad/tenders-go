# 2026-02-25 — Merge-to-New: два сценария слияния дубликатов

## Контекст

Предыдущая реализация `POST /api/v1/admin/merges/:id/execute` поддерживала
только один сценарий: дубликат B вливается в мастер A. Однако на практике
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

`ExecuteMergeHandler` теперь парсит опциональное JSON-тело (`ContentLength > 0`).
Пустой body = Сценарий 1. Тело с `new_main_title` = Сценарий 2.

### 5. Тесты

- 9 существующих тестов обновлены: добавлен 4-й аргумент `""` (Сценарий 1).
- `TestExecuteMerge_Success` проверяет новые поля: `ResultingPositionID`, `Scenario`.
- Все 9 тестов проходят.
- Тесты для Сценария 2 запланированы в TESTING_CHECKLIST.md (7 новых пунктов).

## Затронутые файлы

- `cmd/internal/db/query/catalog_position.sql` — +2 запроса (CreateSimpleCatalogPosition, SetPositionMerged)
- `cmd/internal/db/query/suggested_merges.sql` — обновлён комментарий ExecuteMerge
- `cmd/internal/db/sqlc/*` — перегенерированы (sqlc + mockgen)
- `cmd/internal/api_models/api_models.go` — +ExecuteMergeRequest, расширен ExecuteMergeResponse
- `cmd/internal/services/catalog/catalog_service.go` — два сценария + diagnoseMergeFailure
- `cmd/internal/server/handlers_rag.go` — парсинг опционального тела
- `cmd/internal/services/catalog/catalog_service_test.go` — обновлены под новую сигнатуру
- `TESTING_CHECKLIST.md` — обновлены задачи 2.2 и 5.8

## API контракт

```http
POST /api/v1/admin/merges/:id/execute

# Сценарий 1 — пустой body или {}
→ 200 { scenario: "default", resulting_position_id: <A> }

# Сценарий 2 — с переименованием
{ "new_main_title": "Новое название" }
→ 200 { scenario: "merge_to_new", resulting_position_id: <C> }
```
