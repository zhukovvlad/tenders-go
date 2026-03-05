# 2026-03-05 — Fix: пагинация ListPendingMerges по группам

## Контекст / Проблема

Фронтенд показывал не все варианты слияния для мастер-позиции. Например, для позиции 511
в БД существовали два PENDING-предложения:

| merge_id | main → dup | similarity_score |
|----------|------------|-----------------|
| 465      | 511 → 574  | 0.900319        |
| 509      | 511 → 643  | 0.997068        |

На фронте отображался только вариант 511→643; вариант 511→574 отсутствовал.

### Причина

SQL-запрос `ListPendingMerges` применял `LIMIT/OFFSET` к **плоским строкам**
(отдельным merge-записям), отсортированным по `similarity_score DESC`:

```sql
-- Было:
SELECT ... FROM suggested_merges sm ...
WHERE sm.status = 'PENDING'
ORDER BY sm.similarity_score DESC
LIMIT $1 OFFSET $2;
```

Сервисный слой затем группировал строки по `main_position_id`. Если между merge-записями
одной группы оказывались строки других групп с более высоким score, часть дубликатов
«отрезалась» пагинацией. Строка 511→574 (score 0.900) попадала за пределы LIMIT,
а строка 511→643 (score 0.997) — внутрь. Группа 511 получала только один дубликат.

## Решение

Пагинация перенесена на уровень **групп** (`main_position_id`), а не строк.
Подзапрос сначала выбирает `main_position_id` с применением LIMIT/OFFSET,
затем основной запрос возвращает **все** merge-записи для выбранных групп:

```sql
-- Стало:
SELECT ... FROM suggested_merges sm ...
WHERE sm.status = 'PENDING'
  AND sm.main_position_id IN (
      SELECT sub.main_position_id
      FROM suggested_merges sub
      WHERE sub.status = 'PENDING'
      GROUP BY sub.main_position_id
      ORDER BY MAX(sub.similarity_score) DESC, sub.main_position_id ASC
      LIMIT $1
      OFFSET $2
  )
ORDER BY sm.similarity_score DESC, sm.main_position_id ASC, sm.id ASC;
```

- Подзапрос: выбирает `pageSize` групп, начиная с `offset`, отсортированных по
  максимальному score в группе. При равном score — по `main_position_id ASC`
  (deterministic tiebreaker, предотвращает page drift).
- Основной запрос: возвращает все строки для выбранных групп.
  Порядок: `similarity_score DESC, main_position_id ASC, id ASC` —
  стабильный при одинаковых score.
- Группа 511 теперь **всегда** содержит оба дубликата: 574 и 643.

## Затронутые файлы

| Файл | Изменение |
|------|-----------|
| `cmd/internal/db/query/suggested_merges.sql` | SQL-запрос `ListPendingMerges` — подзапрос с GROUP BY + LIMIT/OFFSET + deterministic tiebreakers |
| `cmd/internal/db/sqlc/*` | Перегенерированы локально (`make sqlc`), не версионируются |
| `cmd/internal/server/handlers_admin.go` | Обновлён doc-комментарий: `page_size` → «количество групп на странице» |
| `cmd/internal/services/catalog/catalog_service.go` | Обновлён doc-комментарий: `pageSize` → «количество групп» |

## Важно

Логика сервисного слоя (`catalog_service.go`) и хандлера (`handlers_admin.go`) **не изменилась** —
обновлены только doc-комментарии, отражающие новую семантику `page_size` (группы вместо строк).
Параметры `Limit`/`Offset` передаются так же — изменилась только семантика:
раньше они ограничивали строки, теперь ограничивают группы.
