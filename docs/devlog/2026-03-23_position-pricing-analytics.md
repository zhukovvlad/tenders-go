# 2026-03-23 — Аналитика цен каталожной позиции

## Контекст

Для оценки конкурентоспособности предложений подрядчиков необходимо видеть историю цен
по конкретной каталожной позиции: кто предлагал, за сколько, в каких тендерах.
Эндпоинт возвращает агрегированную статистику (min/max/avg) по единицам измерения,
а также детальные строки сметы из всех предложений.

## Что сделано

### 1. SQL-запрос GetPositionPricingData (`position_pricing.sql`)

```sql
-- name: GetPositionPricingData :many
SELECT
    t.etp_id AS tender_number,
    l.lot_key,
    c.title AS contractor_title,
    c.inn AS contractor_inn,
    pi.job_title_in_proposal AS original_title,
    pi.quantity,
    pi.unit_cost_materials,
    pi.unit_cost_works,
    pi.unit_cost_indirect_costs,
    pi.unit_cost_total,
    COALESCE(u.normalized_name, 'Не указано') AS unit_name,
    w.rank AS winner_rank,
    w.awarded_share AS winner_share,
    pi.created_at
FROM position_items pi
JOIN proposals p ON pi.proposal_id = p.id
JOIN contractors c ON p.contractor_id = c.id
JOIN lots l ON p.lot_id = l.id
JOIN tenders t ON l.tender_id = t.id
LEFT JOIN units_of_measurement u ON pi.unit_id = u.id
LEFT JOIN winners w ON p.id = w.proposal_id
WHERE
    pi.catalog_position_id = sqlc.arg(catalog_position_id)::bigint
    AND pi.is_chapter = false
    AND p.is_baseline = false
ORDER BY pi.created_at DESC;
```

Ключевые решения:

| Решение | Обоснование |
|---------|-------------|
| `sqlc.arg()::bigint` | Без каста SQLC генерирует `sql.NullInt64` (колонка nullable), каст даёт `int64` |
| `is_chapter = false` | Исключаем заголовки разделов сметы — нужны только реальные позиции |
| `is_baseline = false` | Исключаем цены заказчика (НМЦК) — нужны только предложения подрядчиков |
| `LEFT JOIN winners` | Не все предложения имеют победителя; `INNER JOIN` потерял бы данные |
| `LEFT JOIN units_of_measurement` | `unit_id` может быть NULL; `COALESCE` гарантирует строку |
| Агрегация в Go, не в SQL | Нужны и детальные строки, и агрегаты — один запрос проще двух |

### 2. API модели (`api_models/api_models.go`)

Три DTO-структуры добавлены в `api_models` (по архитектурному стандарту проекта):
- `PositionPriceItem` — одна строка сметы (`*float64` для nullable numeric-полей, `*int` для `winner_rank`)
- `UnitPricingStats` — статистика по ед. изм. (total/winner count, min/max/avg, items[])
- `PositionPricingResponse` — корневой ответ с `map[string]*UnitPricingStats`

### 3. Сервисный слой (`catalog/pricing.go`)

**`GetPositionPricingStats(ctx, catalogPositionID int64) (*api_models.PositionPricingResponse, error)`**:

Алгоритм агрегации в памяти через промежуточный `unitAccumulator`:
1. Запрос `GetPositionPricingData` возвращает плоские строки
2. В цикле строки группируются в `map[unitName]*unitAccumulator`
3. Для каждой строки: инкремент счётчиков, обновление min/max, суммирование `unit_cost_total`
4. Финальный проход: `avg = sum / count`, округление до 2 знаков, сборка `api_models.PositionPricingResponse`

Особенности:
- `unitAccumulator` — локальная структура с `sumTotalCost`/`validCostCount` для промежуточных вычислений (не попадает в API)
- `parseNullNumeric()` — конвертация `sql.NullString` (PostgreSQL numeric) → `*float64`
- Пустые слайсы `Items: []api_models.PositionPriceItem{}` для консистентного JSON (`[]`, не `null`)

### 4. HTTP-хендлер (`handlers_pricing.go`)

**`GetPositionPricingHandler`** — `GET /api/v1/catalog/positions/:id/pricing`:

- Парсит `:id` через `strconv.ParseInt`
- Вызывает `catalogService.GetPositionPricingStats`
- Диспетчеризация ошибок: `NotFoundError` → 404, остальное → 500
- При успехе — 200 с `PositionPricingResponse`

### 5. Маршрут (`server.go`)

```go
protected.GET("/catalog/positions/:id/pricing", server.GetPositionPricingHandler)
```

Добавлен в `protected`-группу (JWT + CSRF). Доступен всем авторизованным пользователям,
не только админам — ценовая аналитика полезна при работе с тендерами.

## Формат JSON-ответа

`GET /api/v1/catalog/positions/:id/pricing` → `200 OK`

```json
{
  "catalog_position_id": 42,
  "stats_by_unit": {
    "м2": {
      "unit_name": "м2",
      "total_count": 5,
      "winner_count": 2,
      "min_unit_cost": 1200.50,
      "max_unit_cost": 3400.00,
      "avg_unit_cost": 2150.75,
      "items": [
        {
          "tender_number": "ETP-2026-001",
          "lot_key": "lot_1",
          "contractor_title": "ООО СтройМастер",
          "contractor_inn": "7701234567",
          "original_title": "Устройство стяжки пола",
          "quantity": 150.0,
          "unit_cost_materials": 800.00,
          "unit_cost_works": 300.50,
          "unit_cost_indirect": 100.00,
          "unit_cost_total": 1200.50,
          "winner_rank": 1,
          "winner_share": 100.00,
          "created_at": "2026-03-20T14:30:00Z"
        },
        {
          "tender_number": "ETP-2026-002",
          "lot_key": "lot_3",
          "contractor_title": "ИП Иванов",
          "contractor_inn": "772987654321",
          "original_title": "Стяжка цементная",
          "quantity": null,
          "unit_cost_materials": null,
          "unit_cost_works": null,
          "unit_cost_indirect": null,
          "unit_cost_total": 3400.00,
          "winner_rank": null,
          "winner_share": null,
          "created_at": "2026-03-18T09:15:00Z"
        }
      ]
    },
    "Не указано": {
      "unit_name": "Не указано",
      "total_count": 1,
      "winner_count": 0,
      "min_unit_cost": 5000.00,
      "max_unit_cost": 5000.00,
      "avg_unit_cost": 5000.00,
      "items": [
        {
          "tender_number": "ETP-2026-003",
          "lot_key": "lot_1",
          "contractor_title": "АО Ремонт",
          "contractor_inn": "7709876543",
          "original_title": "Стяжка (без ед. изм.)",
          "quantity": 1.0,
          "unit_cost_materials": 2000.00,
          "unit_cost_works": 2500.00,
          "unit_cost_indirect": 500.00,
          "unit_cost_total": 5000.00,
          "winner_rank": null,
          "winner_share": null,
          "created_at": "2026-03-15T11:00:00Z"
        }
      ]
    }
  }
}
```

**Типы полей для фронтенда:**

| Поле | Тип | Nullable | Описание |
|------|-----|----------|----------|
| `catalog_position_id` | `number` | нет | ID каталожной позиции |
| `stats_by_unit` | `Record<string, UnitPricingStats>` | нет | Ключ — название ед. изм. |
| `unit_name` | `string` | нет | `"Не указано"` если unit_id отсутствует |
| `total_count` | `number` | нет | Кол-во предложений по данной ед. изм. |
| `winner_count` | `number` | нет | Кол-во предложений от победителей |
| `min_unit_cost` | `number \| null` | да | `null` если все `unit_cost_total` отсутствуют |
| `max_unit_cost` | `number \| null` | да | аналогично |
| `avg_unit_cost` | `number \| null` | да | Округлено до 2 знаков |
| `quantity` | `number \| null` | да | Количество по ТЗ |
| `unit_cost_materials` | `number \| null` | да | Стоимость материалов за единицу |
| `unit_cost_works` | `number \| null` | да | Стоимость работ за единицу |
| `unit_cost_indirect` | `number \| null` | да | Накладные расходы за единицу |
| `unit_cost_total` | `number \| null` | да | Итого за единицу |
| `winner_rank` | `number \| null` | да | Ранг победителя (1, 2, 3...) |
| `winner_share` | `number \| null` | да | Доля победителя в % |
| `created_at` | `string` | нет | ISO 8601 / RFC 3339 |

## Архитектурные решения

| Решение | Обоснование |
|---------|-------------|
| Метод на `CatalogService`, не новый сервис | Аналитика каталожной позиции — домен каталога; не требует изменений в `Server`, `NewServer`, `app.go` |
| Отдельный файл `pricing.go` | Чёткое разделение ответственности внутри пакета `catalog` |
| DTO в `api_models`, не в сервисе | Стандарт проекта: все ответные структуры в одном месте; сервис возвращает `api_models.*` |
| `unitAccumulator` в сервисе | Промежуточные поля `sumTotalCost`/`validCostCount` не попадают в API-модели |
| `protected`, не `admin` | Ценовая аналитика — рабочий инструмент, не административная функция |
| Указатели `*float64`, `*int` | PostgreSQL `numeric` и `int` могут быть NULL; указатели → `null` в JSON |
| `parseNullNumeric` | SQLC генерирует `sql.NullString` для `numeric`; нужна конвертация в `*float64` |
| Округление `math.Round(avg*100)/100` | Две значащие цифры после запятой — стандарт для денежных значений |

## Затронутые файлы

- `cmd/internal/db/query/position_pricing.sql` — новый SQL-запрос `GetPositionPricingData`
- `cmd/internal/db/sqlc/position_pricing.sql.go` — перегенерирован (`make sqlc`)
- `cmd/internal/db/sqlc/querier.go` — перегенерирован
- `cmd/internal/db/sqlc/mock_querier.go` — перегенерирован (`mockgen`)
- `cmd/internal/db/sqlc/mock_store.go` — перегенерирован (`mockgen`)
- `cmd/internal/api_models/api_models.go` — новые DTO: `PositionPriceItem`, `UnitPricingStats`, `PositionPricingResponse`
- `cmd/internal/services/catalog/pricing.go` — новый файл: `unitAccumulator` + `GetPositionPricingStats` + `parseNullNumeric`
- `cmd/internal/server/handlers_pricing.go` — новый хендлер `GetPositionPricingHandler`
- `cmd/internal/server/server.go` — новый маршрут `GET /catalog/positions/:id/pricing`
