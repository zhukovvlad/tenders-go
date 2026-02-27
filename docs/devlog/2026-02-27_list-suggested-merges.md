# 2026-02-27 — GET /suggested_merges (список предложений о слиянии)

## Контекст

SQL-запрос `ListPendingMerges` существовал в `suggested_merges.sql` с момента создания
таблицы, но не был подключён ни к сервисному слою, ни к HTTP API.
Администратору нужен эндпоинт для просмотра очереди PENDING merge-предложений,
сгруппированных по мастер-позиции, чтобы принимать решения о слиянии.

## Что сделано

### 1. API Models (`api_models.go`)

Добавлены DTO для ответа:

| Структура | Назначение |
|-----------|-----------|
| `CatalogPositionSummary` | Краткая информация о позиции каталога (без embedding/fts_vector) |
| `SuggestedMergeItem` | Одна merge-запись: merge_id, similarity_score, дубликат, created_at |
| `SuggestedMergeGroup` | Группа: мастер-позиция + массив её дубликатов |
| `ListSuggestedMergesResponse` | Ответ: `groups`, `total` (все PENDING), `total_groups` (уникальные main_position_id) |

`CatalogPositionSummary` содержит только `id`, `standard_job_title`, `description` (nullable → `*string`),
`kind`, `status` — без тяжёлых полей `embedding`, `fts_vector`, `merged_into_id`.

### 2. SQL: `CountPendingMerges` + `CountPendingMergeGroups` (`suggested_merges.sql`)

Добавлены два запроса для пагинации:

```sql
-- Общее количество PENDING merge-записей
SELECT COUNT(*) FROM suggested_merges WHERE status = 'PENDING';

-- Количество уникальных групп (мастер-позиций)
SELECT COUNT(DISTINCT main_position_id) FROM suggested_merges WHERE status = 'PENDING';
```

- `Total` — общее число PENDING merge-записей («37 предложений»).
- `TotalGroups` — сколько уникальных кластеров дубликатов («12 групп»).

### 3. Service Layer (`catalog/catalog_service.go`)

Добавлен метод `ListPendingMerges(ctx, page, pageSize)`:

- **Валидация**: `page >= 1`, `pageSize` от 1 до 500 → `ValidationError`.
- **Подсчёт**: `store.CountPendingMerges()` → `Total`, `store.CountPendingMergeGroups()` → `TotalGroups`.
- **Данные**: `store.ListPendingMerges()` (JOIN с catalog_positions, LIMIT/OFFSET).
- **Группировка**: результаты группируются по `main_position_id` с сохранением порядка появления.
  Используется `groupOrder []int64` + `groupMap` для стабильного порядка групп.
- **Конвертация**: helper `catalogPositionToSummary()` конвертирует `db.CatalogPosition` →
  `api_models.CatalogPositionSummary` (nullable `sql.NullString` → `*string`).

### 4. HTTP Handler (`handlers_admin.go`)

Добавлен `ListSuggestedMergesHandler`:

- **Query-параметры**: `page` (default 1), `page_size` (default 100).
- **Ошибки**: `ValidationError` → 400, прочие → 500.
- Паттерн аналогичен `listTenderCategoriesHandler` / `HandleListSystemSettings`.

### 5. Routing (`server.go`)

Маршрут `admin.GET("/suggested_merges", server.ListSuggestedMergesHandler)` добавлен
в группу admin (за `RequireRole("admin")` middleware), перед merges/execute-batch.

## Пример ответа

```json
{
  "groups": [
    {
      "main_position": {
        "id": 1,
        "standard_job_title": "Монтаж кабеля",
        "description": "Прокладка силового кабеля...",
        "kind": "work",
        "status": "active"
      },
      "merges": [
        {
          "merge_id": 10,
          "similarity_score": 0.95,
          "duplicate": {
            "id": 2,
            "standard_job_title": "Прокладка кабеля силового",
            "kind": "work",
            "status": "active"
          },
          "created_at": "2026-02-27T10:00:00Z"
        },
        {
          "merge_id": 11,
          "similarity_score": 0.88,
          "duplicate": {
            "id": 5,
            "standard_job_title": "Монтаж кабельной линии",
            "kind": "work",
            "status": "active"
          },
          "created_at": "2026-02-27T10:05:00Z"
        }
      ]
    }
  ],
  "total": 42,
  "total_groups": 12
}
```

- `total` — общее количество PENDING merge-записей в БД.
- `total_groups` — количество уникальных мастер-позиций (кластеров дубликатов).

## Файлы затронуты

- `cmd/internal/db/query/suggested_merges.sql` — добавлены `CountPendingMerges`, `CountPendingMergeGroups`
- `cmd/internal/db/sqlc/*` — автогенерация (`make sqlc`)
- `cmd/internal/api_models/api_models.go` — добавлены `CatalogPositionSummary`, `SuggestedMergeItem`, `SuggestedMergeGroup`, `ListSuggestedMergesResponse`
- `cmd/internal/services/catalog/catalog_service.go` — добавлен `ListPendingMerges`, `catalogPositionToSummary`
- `cmd/internal/server/handlers_admin.go` — добавлен `ListSuggestedMergesHandler`
- `cmd/internal/server/server.go` — маршрут `GET /suggested_merges` в admin-группе
- `TESTING_CHECKLIST.md` — обновлён
- `docs/devlog/2026-02-27_list-suggested-merges.md` — этот файл
