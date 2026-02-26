# 2026-02-26 — Таблица system_settings + миграция Go с snap на tarball

## Контекст

«Волшебные» константы (например, порог дедупликации `DEDUP_DISTANCE_THRESHOLD = 0.15`)
хранились в коде Python-воркера. Изменение требовало редеплоя. Нужна возможность менять
параметры системы из админки без перезапуска сервисов.

## Что сделано

### 1. Миграция 000005 — `system_settings`

**Таблица:**
- `key VARCHAR(50) PRIMARY KEY` — ключ настройки
- `value_numeric`, `value_string`, `value_boolean` — типизированные значения
- `description` — описание (для UI)
- `created_at`, `updated_at`, `updated_by` — аудит

**Улучшения относительно исходного плана:**
1. Добавлен `created_at` — консистентность со всеми таблицами проекта
2. `(now())` — стиль скобок как в 000001
3. CHECK constraint `ck_system_settings_has_value` — гарантия, что ровно одна value-колонка заполнена (`num_nonnulls() = 1`)
4. Trigger `trg_system_settings_updated_at` — автообновление `updated_at` при UPDATE (не перекладываем на приложение)

**Seed-данные:**
```sql
INSERT INTO system_settings (key, value_numeric, description, updated_by)
VALUES ('dedup_distance_threshold', 0.15, 'Порог косинусного расстояния для AI-поиска дубликатов (меньше = строже)', 'system');
```

**DOWN-миграция:**
- DROP TRIGGER + DROP FUNCTION + DROP TABLE

### 2. SQLC-запросы (`system_settings.sql`)

| Запрос | Тип | Описание |
|--------|-----|----------|
| `GetSystemSettingByKey` | `:one` | Получение по ключу |
| `ListSystemSettings` | `:many` | Все настройки (ORDER BY key) |
| `UpsertSystemSettingNumeric` | `:one` | Upsert числовой (обнуляет string/boolean) |
| `UpsertSystemSettingString` | `:one` | Upsert текстовой (обнуляет numeric/boolean) |
| `UpsertSystemSettingBoolean` | `:one` | Upsert булевой (обнуляет numeric/string) |
| `DeleteSystemSetting` | `:exec` | Удаление по ключу |

Upsert-запросы типобезопасны: при смене типа значения другие value-колонки обнуляются.
`description` сохраняется через `COALESCE` при upsert без явного описания.

### 3. Миграция Go с snap на tarball

**Проблема:** `mockgen` кэширует абсолютный путь к `go` в бинарнике.
Snap автообновляет Go (новая ревизия), удаляя старую → `mockgen` вызывает
`/snap/go/11017/bin/go` — path not found. Проблема всплывает при каждом
обновлении snap (раз в несколько недель).

**Решение:**
1. `sudo snap remove go`
2. Скачан tarball Go 1.26.0 с go.dev
3. `sudo tar -C /usr/local -xzf go1.26.0.linux-amd64.tar.gz`
4. `export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH` добавлен в `~/.bashrc`
5. `go install go.uber.org/mock/mockgen@latest` и `sqlc@latest`

Теперь `go` живёт по стабильному пути `/usr/local/go/bin/go`, snap-ревизии
не будут ломать toolchain.

### 4. Генерация кода

- `make sqlc` — sqlc generate + mockgen для querier и store — чисто, без ошибок
- Сгенерированы: `system_settings.sql.go`, модель `SystemSetting`, методы в `querier.go`
- Моки обновлены (`mock_querier.go`, `mock_store.go`)

### 5. Тесты

- `make test` — все существующие тесты проходят (exit 0)
- Добавлены пункты в `TESTING_CHECKLIST.md`:
  - Задача 4.5: тест миграции 000005
  - Задача 4.6: интеграционные тесты для всех system_settings queries (13 пунктов)

## Код-ревью (Copilot)

### Принятые замечания (3)
1. **UP: exactly-one-value constraint** — `OR` (хотя бы одна) → `num_nonnulls(...) = 1` (ровно одна).
   Исключает неоднозначность при одновременном заполнении нескольких value-колонок.
2. **DOWN: reorder для идемпотентности** — `DROP TRIGGER ON system_settings` упадёт если таблица уже удалена.
   Исправлено: сначала `DROP TABLE CASCADE` (удаляет триггер автоматически), потом `DROP FUNCTION`.
3. **Devlog: пунктуация** — добавлена запятая в описании CHECK constraint.

Применение: rollback 000005 → правка файлов → reapply 000005.

## Файлы затронуты

- `cmd/internal/db/migration/000005_add_system_settings.up.sql` (новый)
- `cmd/internal/db/migration/000005_add_system_settings.down.sql` (новый)
- `cmd/internal/db/query/system_settings.sql` (новый)
- `cmd/internal/db/sqlc/*` (автогенерация)
- `TESTING_CHECKLIST.md` (обновлён)
- `~/.bashrc` (Go PATH)
