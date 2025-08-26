# 🎯 Упрощенный API для AI результатов (только DB ID)

## Проблема с ETP ID

Из логов видно, что Python отправляет DB ID вместо ETP ID:

```
POST /api/v1/tenders/134/lots/134/ai-results
```

Где `134` - это внутренний DB ID, а не ETP ID тендера.

## 💡 Решение: Новый упрощенный endpoint

### Endpoint

```
POST /api/v1/lots/{lot_id}/ai-results
```

### Описание

- Принимает только `lot_id` (DB ID лота)
- Не требует знания `tender_id` или ETP ID
- Работает напрямую с базой данных
- Упрощает интеграцию с Python сервисом

### Формат запроса

```json
{
  "lot_key_parameters": {
    "ai": {
      "source": "gemini",
      "category": "construction",
      "data": {
        /* результаты AI анализа */
      },
      "processed_at": "2024-01-01T12:00:00Z"
    }
  }
}
```

### Пример использования

```bash
# Для лота с ID 134
curl -X POST http://localhost:8080/api/v1/lots/134/ai-results \
  -H "Content-Type: application/json" \
  -d @example_simple_ai_result.json
```

### Возможные ответы

#### 200 OK - Успешное обновление

```json
{
  "message": "AI результаты успешно обработаны",
  "lot_id": "134",
  "updated_at": "now"
}
```

#### 400 Bad Request - Ошибка валидации

```json
{
  "error": "ключевые параметры (lot_key_parameters) не могут быть пустыми"
}
```

#### 404 Not Found - Лот не найден

```json
{
  "error": "лот с ID 134 не найден"
}
```

## 🔄 Логика работы

1. **Извлечение lot_id** из URL параметра
2. **Валидация** JSON данных
3. **Поиск лота** в БД по ID (без проверки tender_id)
4. **Обновление** `lot_key_parameters` в транзакции
5. **Возврат** результата операции

## ⚡ Преимущества

- ✅ **Простота**: только один ID вместо двух
- ✅ **Быстрота**: нет лишних проверок tender_id
- ✅ **Надежность**: работает с фактическими DB ID
- ✅ **Совместимость**: подходит для Python сервиса

## 🔧 Интеграция с Python

Python сервис может использовать упрощенный формат:

```python
# Вместо сложного URL:
# POST /api/v1/tenders/{tender_etp_id}/lots/{lot_id}/ai-results

# Используем простой:
POST /api/v1/lots/{lot_id}/ai-results

# С упрощенным payload (без tender_id и lot_id в JSON):
{
  "lot_key_parameters": { /* AI результаты */ }
}
```

## 📋 Сравнение endpoints

| Параметр       | Полный endpoint                                 | Упрощенный endpoint         |
| -------------- | ----------------------------------------------- | --------------------------- |
| URL            | `/tenders/{tender_id}/lots/{lot_id}/ai-results` | `/lots/{lot_id}/ai-results` |
| tender_id      | Обязателен (ETP ID)                             | Не нужен                    |
| lot_id         | Обязателен (DB ID)                              | Обязателен (DB ID)          |
| Проверка связи | Да (лот ∈ тендер)                               | Нет                         |
| JSON payload   | Полный                                          | Упрощенный                  |
| Время отклика  | Медленнее                                       | Быстрее                     |

## 🎯 Рекомендация

**Использовать упрощенный endpoint** `POST /api/v1/lots/{lot_id}/ai-results` для:

- Python сервиса (у него есть только DB ID лотов)
- Быстрого обновления AI результатов
- Упрощения логики интеграции

**Оставить полный endpoint** для случаев, когда нужна дополнительная валидация принадлежности лота тендеру.
