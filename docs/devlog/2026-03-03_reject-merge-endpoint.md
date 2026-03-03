# 2026-03-03 — Эндпоинт отклонения merge-предложений (Reject Merge)

## Контекст

При работе с UI оператор мог исполнять (`POST /merges/:id/execute`) предложения о слиянии,
но возможность отклонить предложение отсутствовала — `PATCH /api/v1/admin/merges/:id/reject`
возвращал 404, т.к. маршрут не был зарегистрирован в роутере Gin.

SQL-запрос `UpdateMergeStatus` (поддерживающий перевод в `REJECTED`) уже существовал
в `suggested_merges.sql`, но не был обёрнут в сервисный метод и хендлер.

## Что сделано

### 1. `CatalogService.RejectMerge` (`catalog_service.go`)

Новый метод сервисного слоя:

1. Валидирует `rejectedBy` (обязательный, берётся из JWT)
2. Атомарно переводит merge из PENDING в REJECTED через `RejectPendingMerge` (SQL с guard `AND status = 'PENDING'`)
3. При `sql.ErrNoRows` — различает «не найден» и «не в PENDING» через fallback `GetSuggestedMergeByID`

Ошибки:
- `NotFoundError` → 404 (merge не найден)
- `ValidationError` → 400 (не PENDING статус, пустой rejectedBy)
- Прочие → 500

### 2. `RejectMergeHandler` (`handlers_rag.go`)

Хендлер `PATCH /api/v1/admin/merges/:id/reject`:

- Парсит `id` из URL (int64)
- Извлекает `user_id` из JWT-контекста (ставится `RequireRole("admin")`)
- Вызывает `CatalogService.RejectMerge`
- Возвращает `200 { "status": "rejected", "merge_id": <id> }`

### 3. Регистрация маршрута (`server.go`)

Добавлен в группу `admin`:

```go
admin.PATCH("/merges/:id/reject", server.RejectMergeHandler)
```

## Файлы затронуты

- `cmd/internal/services/catalog/catalog_service.go` — добавлен `RejectMerge`
- `cmd/internal/server/handlers_rag.go` — добавлен `RejectMergeHandler`
- `cmd/internal/server/server.go` — зарегистрирован маршрут
