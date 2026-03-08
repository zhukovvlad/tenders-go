# 2026-03-08 — Исправление CreateSimpleCatalogPosition: запись description

## Контекст

При создании новой каталожной позиции в сценарии Merge-to-New (`ExecuteMerge` / `ExecuteBatchMerge`,
сценарий 2) вызывается `CreateSimpleCatalogPosition`. Ранее запрос записывал введённое оператором
название только в поле `standard_job_title`, оставляя `description = NULL`.

Это было архитектурно неверно: в нашей модели данных `description` — **первичное** поле
(сырой текст от пользователя), а `standard_job_title` — **производное** (лемматизированная
нормальная форма, которую Python NLP-воркер создаёт из `description`).

## Проблема

Позиция создавалась с `description = NULL` и `standard_job_title = <ввод оператора>`.
Воркер, получив позицию в статусе `pending_indexing`, читает `description` для лемматизации —
при `NULL` он либо падал с ошибкой, либо оставлял `standard_job_title` как есть без обработки.

## Решение

Передавать введённый оператором текст в оба поля при создании:

```sql
INSERT INTO catalog_positions (
    standard_job_title,
    description,
    kind,
    status
) VALUES (
    $1,                  -- standard_job_title: временное значение, воркер заменит на лемматизированную форму
    $1,                  -- description: исходный текст от оператора (первичное поле)
    'POSITION',
    'pending_indexing'
)
```

Воркер при обработке позиции со статусом `pending_indexing`:
1. Читает `description` как источник.
2. Лемматизирует и записывает результат в `standard_job_title`.
3. Генерирует эмбеддинг и переводит статус в `active`.

## Что изменено

- `cmd/internal/db/query/catalog_position.sql` — добавлен `description = $1` в `CreateSimpleCatalogPosition`.
- `cmd/internal/db/sqlc/catalog_position.sql.go` — пересоздан через `make sqlc`.

## Затронутые сценарии

- `ExecuteMerge` Сценарий 2 (Merge-to-New): создание позиции C.
- `ExecuteBatchMerge` Сценарий 2: создание позиции C для каждой группы.

## Совместимость

Сигнатура Go-функции `CreateSimpleCatalogPosition(ctx, standardJobTitle string)` **не изменилась**
— оба поля заполняются одним параметром `$1`. Существующие тесты и моки не требуют правок.
