# 2026-03-06 — GROUP_TITLE: новый kind для родительских групп

## Контекст

В рамках Position Grouping (2026-03-05) абстрактные родители создавались с `kind='HEADER'`.
Это вызывало семантический конфликт: `HEADER` уже используется для распарсенных заголовков
тендерных документов. Кроме того, родитель сразу получал `status='active'`, хотя для
корректной работы RAG-поиска Python NLP-воркер должен лемматизировать название и сгенерировать
эмбеддинг.

### Решение

1. Ввести новый `kind = 'GROUP_TITLE'` — явное обозначение пользовательских родительских групп.
2. Создавать родителя со `status = 'pending_indexing'` — воркер обработает его как обычную позицию.
3. Сырой пользовательский ввод записывается и в `standard_job_title`, и в `description` —
   воркер лемматизирует `standard_job_title`, а `description` сохраняет оригинал для UI.

## Что сделано

### 1. Миграция 000008: `GROUP_TITLE` в constraint

**Файл:** `cmd/internal/db/migration/000008_add_group_kind.up.sql`

```sql
ALTER TABLE catalog_positions DROP CONSTRAINT ck_catalog_positions_kind;
ALTER TABLE catalog_positions ADD CONSTRAINT ck_catalog_positions_kind
  CHECK (kind IN ('POSITION', 'HEADER', 'LOT_HEADER', 'TRASH', 'TO_REVIEW', 'GROUP_TITLE'));
```

`HEADER` сохранён для обратной совместимости с существующими данными.

### 2. SQL-запросы (обновлённые)

**`catalog_position.sql` — `CreateParentCatalogPosition`:**

```sql
INSERT INTO catalog_positions (standard_job_title, description, kind, status)
VALUES ($1, $1, 'GROUP_TITLE', 'pending_indexing')
RETURNING *;
```

Ключевые изменения:
- `kind`: `HEADER` → `GROUP_TITLE`
- `status`: `active` → `pending_indexing` (отправляется в очередь воркера)
- Добавлено поле `description` = `$1` (сырой ввод для UI)

**`catalog_position.sql` — `ListCatalogPositionsForEmbedding`:**

```sql
SELECT id, standard_job_title, description, kind
FROM catalog_positions
WHERE status = 'pending_indexing'
  AND kind IN ('POSITION', 'GROUP_TITLE')
ORDER BY id LIMIT $1;
```

Ключевые изменения:
- `kind = 'POSITION'` → `kind IN ('POSITION', 'GROUP_TITLE')` — воркер теперь обрабатывает и группы
- Добавлено поле `kind` в SELECT — воркер может различать типы позиций

### 3. Go-код (`catalog_service.go`)

В `resolveParentID` обновлена валидация:

```go
// Было:
if parent.Kind != "HEADER" { ... "kind=HEADER" }
// Стало:
if parent.Kind != "GROUP_TITLE" { ... "kind=GROUP_TITLE" }
```

### 4. Пересборка

- `make sqlc` — перегенерация Go-кода (sqlc + mockgen)
- `go build ./cmd/...` — компиляция без ошибок
- `make migrateup` — миграция применена

## Влияние на существующие данные

Существующие `HEADER`-записи остаются валидными (constraint сохраняет `HEADER`).
Новые родительские группы будут создаваться с `GROUP_TITLE`.
При необходимости можно добавить data-миграцию `UPDATE ... SET kind='GROUP_TITLE' WHERE kind='HEADER'`
для унификации старых записей.

## Влияние на Python-воркер

Воркер получает `GROUP_TITLE`-записи через `ListCatalogPositionsForEmbedding`.
Поле `kind` теперь доступно в ответе — воркер может при необходимости применять
разную логику лемматизации для `POSITION` и `GROUP_TITLE`.
