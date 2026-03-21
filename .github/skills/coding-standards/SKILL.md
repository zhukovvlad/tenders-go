---
name: coding-standards
description: "WORKFLOW SKILL — Проектирование и кодирование новых функций в tenders-go по принятым в индустрии стандартам. USE FOR: добавление новых сервисов, хендлеров, middleware, моделей, SQL-запросов, миграций; рефакторинг существующего кода; код-ревью PR. DO NOT USE FOR: написание тестов (используй testing-go-app); конфигурация инфраструктуры (Docker/CI); работа с Python-воркерами. INVOKES: file-read tools (read existing patterns), run_in_terminal (go build/vet/lint), subagents (codebase exploration)."
---

# Coding Standards: tenders-go

## When to Use

Use this skill when:

- Добавляется новый домен (сервис, хендлер, SQLC-запрос, миграция).
- Необходимо привести код к стандартам проекта.
- Проводится код-ревью PR на соответствие архитектурным паттернам.
- Рефакторинг затрагивает слои (HTTP → Service → DB).

Do NOT use for:
- Написание тестов → используй skill `testing-go-app`.
- Настройка CI/CD или Docker-окружения.
- Python-воркеры и скрипты парсинга.

---

## Role

Ты — senior Go-инженер с опытом построения production-систем на:
- **Go 1.24+** (Gin, SQLC, pgvector, errgroup)
- **PostgreSQL** (транзакции, JSONB, индексы, миграции)
- **Чистая архитектура** (HTTP → Service → Store)
- **Безопасность**: JWT-cookie auth, CSRF, rate limiting, RBAC

Ты пишешь только тот код, который решает задачу. Никакого over-engineering.

---

## Module & Project Essentials

```
Module: github.com/zhukovvlad/tenders-go
Go:     1.24+
Router: github.com/gin-gonic/gin
DB:     database/sql + SQLC + pgvector
Auth:   JWT (httpOnly cookie) + CSRF (double-submit cookie)
Log:    cmd/pkg/logging (custom Logger interface)
```

---

# Architecture: Layers & Rules

```
HTTP Layer       cmd/internal/server/handlers_*.go
    ↓ (только DTOs, никакой бизнес-логики)
Service Layer    cmd/internal/services/<domain>/
    ↓ (только domain logic, никакого HTTP)
Store Layer      cmd/internal/db/sqlc/ (SQLC-generated)
    ↓
PostgreSQL
```

**Железные правила:**

1. Хендлеры **не содержат** бизнес-логику — только парсинг, вызов сервиса, маппинг ответа.
2. Сервисы **не знают** о `*gin.Context` и HTTP-статусах.
3. Store — **только SQLC-сгенерированный код**. Кастомная логика живёт в сервисах.
4. Dependency Injection — **только через конструкторы** `New<Type>`.
5. Всё, что пересекает слои — через **интерфейсы** (особенно `db.Store`, `logging.Logger`).

---

# Workflow (Required Sequence)

Всегда следуй этой последовательности при реализации новой функции:

---

## Step 1. Explore Existing Patterns

Перед написанием ANY кода:

1. Найди аналогичный существующий домен (например, `handlers_lot.go` как шаблон для нового хендлера лотов).
2. Прочитай структуры запросов/ответов в этом файле.
3. Проверь, есть ли уже нужный SQL-запрос в `cmd/internal/db/query/`.
4. Прочитай `cmd/main/app.go` чтобы понять точку сборки.

```bash
# Список всех хендлеров
ls cmd/internal/server/handlers_*.go

# Список SQL-запросов
ls cmd/internal/db/query/

# Список сервисов
ls cmd/internal/services/
```

**Никогда не пиши код по памяти — всегда читай существующий код как эталон.**

---

## Step 2. Database Layer (if needed)

### 2a. SQL Migration

Новая таблица или изменение схемы = новый файл миграции:

```
cmd/internal/db/migration/000XXX_<description>.up.sql
cmd/internal/db/migration/000XXX_<description>.down.sql
```

**Правила миграций:**
- Номер = следующий по порядку (проверь существующие).
- `.down.sql` ДОЛЖЕН корректно откатывать `.up.sql`.
- Используй `IF NOT EXISTS` / `IF EXISTS` для идемпотентности.
- Добавляй индексы рядом с таблицей в том же файле миграции.
- Внешние ключи — всегда с явным `ON DELETE` / `ON UPDATE`.

### 2b. SQL Query

Новый запрос в `cmd/internal/db/query/<entity>.sql`:

```sql
-- name: <ActionEntity> :one|:many|:exec
-- Всегда используй именованные параметры sqlc (sqlc.arg(name))
SELECT id, etp_id, title
FROM tenders
WHERE id = sqlc.arg(id);
```

**SQLC аннотации:**
- `:one` — возвращает одну строку, ошибка если не найдено.
- `:many` — возвращает слайс.
- `:exec` — без возвращаемого значения (INSERT/UPDATE/DELETE).
- `:execresult` — возвращает `sql.Result`.

После изменения SQL-файлов обязательно регенерируй:

```bash
make sqlc-generate
# или
sqlc generate
```

---

## Step 3. Service Layer

### Структура файла

```
cmd/internal/services/<domain>/
    <domain>_service.go     # основной сервис
    <domain>_service_test.go # unit-тесты (если нужны)
```

### Шаблон сервиса

```go
package <domain>

import (
    "context"

    db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
    "github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

type <Domain>Service struct {
    store  db.Store
    logger logging.Logger
}

func New<Domain>Service(store db.Store, logger logging.Logger) *<Domain>Service {
    return &<Domain>Service{
        store:  store,
        logger: logger,
    }
}

func (s *<Domain>Service) <Action>(ctx context.Context, params ...) (<Result>, error) {
    logger := s.logger.WithField("method", "<Action>")

    // 1. Валидация входных параметров
    if params == nil {
        return nil, apierrors.NewValidationError("<field> is required")
    }

    // 2. Бизнес-логика

    // 3. Работа с DB (через s.store или s.store.ExecTx)
    result, err := s.store.<Query>(ctx, db.<Params>{...})
    if err != nil {
        logger.WithError(err).Error("failed to <action>")
        return nil, err
    }

    return result, nil
}
```

### Правила сервисного слоя

- Логируй **начало** каждого метода через `logger := s.logger.WithField("method", "...")`.
- Возвращай кастомные ошибки из `apierrors` для всех предсказуемых случаев.
- Для операций, затрагивающих несколько таблиц, используй `s.store.ExecTx`:

```go
return s.store.ExecTx(ctx, func(qtx *db.Queries) error {
    // Все операции внутри транзакции
    if _, err := qtx.CreateTender(ctx, tenderParams); err != nil {
        return err
    }
    return qtx.CreateLot(ctx, lotParams)
})
```

---

## Step 4. HTTP Handler Layer

### Структура файла

```
cmd/internal/server/handlers_<domain>.go
```

### Шаблон хендлера

```go
// Request/Response DTO — определяй рядом с хендлером
type <action>Request struct {
    Field string `json:"field" binding:"required"`
}

type <entity>Response struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
}

// Хендлер — метод на *Server
func (s *Server) <action>Handler(c *gin.Context) {
    // 1. Извлечь auth-контекст если нужен
    userID := c.GetString("user_id")

    // 2. Парсинг path params
    id, err := strconv.ParseInt(c.Param("id"), 10, 64)
    if err != nil {
        c.JSON(http.StatusBadRequest, errorResponse(err))
        return
    }

    // 3. Bind body (для POST/PUT)
    var req <action>Request
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, errorResponse(err))
        return
    }

    // 4. Вызов сервиса
    result, err := s.<domainService>.<Action>(c.Request.Context(), ...)
    if err != nil {
        s.handleServiceError(c, err)
        return
    }

    // 5. Маппинг в response DTO и отправка
    c.JSON(http.StatusOK, <entity>Response{
        ID:   result.ID,
        Name: result.Name,
    })
}
```

### Обработка ошибок сервиса в хендлерах

Используй следующий паттерн (или вынеси в `handleServiceError`):

```go
switch e := err.(type) {
case *apierrors.ValidationError:
    c.JSON(http.StatusBadRequest, errorResponse(err))
case *apierrors.NotFoundError:
    c.JSON(http.StatusNotFound, errorResponse(err))
case *apierrors.ConflictError:
    c.JSON(http.StatusConflict, gin.H{"error": e.Message, "conflicts": e.Conflicts})
default:
    s.logger.WithError(err).Error("unexpected error in handler")
    c.JSON(http.StatusInternalServerError, errorResponse(err))
}
```

### Пагинация (стандарт)

```go
pageIDStr := c.DefaultQuery("page", "1")
pageSizeStr := c.DefaultQuery("page_size", "20")
pageID, err := strconv.ParseInt(pageIDStr, 10, 32)
pageSize, err := strconv.ParseInt(pageSizeStr, 10, 32)

params := db.List<Entity>Params{
    Limit:  int32(pageSize),
    Offset: (int32(pageID) - 1) * int32(pageSize),
}
```

### Параллельная загрузка данных

```go
var (
    items []db.Item
    count int64
)
eg, egCtx := errgroup.WithContext(c.Request.Context())
eg.Go(func() error {
    var err error
    items, err = s.store.ListItems(egCtx, params)
    return err
})
eg.Go(func() error {
    var err error
    count, err = s.store.CountItems(egCtx)
    return err
})
if err := eg.Wait(); err != nil {
    c.JSON(http.StatusInternalServerError, errorResponse(err))
    return
}
```

---

## Step 5. Register Route in server.go

Открой `cmd/internal/server/server.go` и добавь маршрут в правильную группу:

```go
// Сгруппированные маршруты по домену
<domainGroup> := apiV1.Group("/<domain>")
{
    <domainGroup>.GET("", s.<list>Handler)
    <domainGroup>.GET("/:id", s.<get>Handler)
    <domainGroup>.POST("", s.<create>Handler)
    <domainGroup>.PUT("/:id", s.<update>Handler)
    <domainGroup>.DELETE("/:id", s.<delete>Handler)
}
```

Для защищённых маршрутов используй middleware-группу:

```go
authorized := apiV1.Group("/")
authorized.Use(AuthMiddleware(s.config, s.store, s.logger))
authorized.Use(CsrfMiddleware())
{
    authorized.<DOMAIN>.GET(...)
}
```

---

## Step 6. Wire in app.go

В `cmd/main/app.go` добавь инициализацию нового сервиса:

```go
<domain>Service := <domain>.New<Domain>Service(store, logger)

server := server.NewServer(
    // ...существующие сервисы...
    server.With<Domain>Service(<domain>Service),
)
```

Если `Server` не использует functional options — добавь поле напрямую в структуру `Server`.

---

## Step 7. Build & Validate

```bash
# Убедись, что код компилируется
go build ./...

# Статический анализ
go vet ./...

# Линтер (если настроен в Makefile)
make lint

# Запусти существующие тесты чтобы не сломать ничего
make test-unit
```

Никогда не коммить код, который не проходит `go build` и `go vet`.

---

# Naming Conventions (Mandatory)

| Элемент | Формат | Пример |
|---------|--------|--------|
| Файл хендлеров | `handlers_<domain>.go` | `handlers_tender.go` |
| Файл middleware | `middleware_<type>.go` | `middleware_auth.go` |
| Файл сервиса | `<domain>_service.go` | `catalog_service.go` |
| Имя хендлера | `(s *Server) <action>Handler` | `listTendersHandler` |
| Имя сервиса (тип) | `<Domain>Service` | `CatalogService` |
| Конструктор | `New<Type>` | `NewCatalogService` |
| Request DTO | `<action>Request` | `createTenderRequest` |
| Response DTO | `<entity>Response` | `LotResponse` |
| Middleware-фабрика | `<Feature>Middleware(...)` | `AuthMiddleware` |
| SQL query name | `<Action><Entity>` | `ListActiveTenders` |

---

# Error Handling: Rules

## Создание ошибок (только в service layer)

```go
import "github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"

// 400
return apierrors.NewValidationError("field '%s' is required", field)

// 404
return apierrors.NewNotFoundError("tender %d not found", id)

// 409
return apierrors.NewConflictError("duplicate entry", conflicts)
```

## Запрещено

- `errors.New("...")` для пользовательских ошибок (используй apierrors).
- `panic` в production-коде (только в middleware при отсутствии критической конфигурации — `GO_SERVER_API_KEY`).
- Игнорирование `err` через `_` без явного комментария `// intentionally ignored`.
- Логирование ошибки И возврат её одновременно в нескольких местах (log once, return always).

---

# Security Checklist (Per Feature)

Перед любым PR проверь:

- [ ] **Authentication**: эндпоинт защищён `AuthMiddleware` (если требует авторизации).
- [ ] **CSRF**: мутирующие операции (POST/PUT/DELETE) требуют `CsrfMiddleware`.
- [ ] **Authorization (RBAC)**: проверяется `role` из контекста если endpoint — admin-only.
- [ ] **Input validation**: `ShouldBindJSON` с `binding:"required"` тегами.
- [ ] **SQL Injection**: только SQLC-параметризованные запросы, никакой конкатенации строк SQL.
- [ ] **Sensitive data**: пароли хэшируются (bcrypt), JWT-секреты из `config`, не в коде.
- [ ] **Rate limiting**: публичные/внешние эндпоинты имеют rate-limit middleware.
- [ ] **Service-to-service auth**: внутренние endpoints защищены `ServiceBearerAuthMiddleware`.

---

# JSON & Converters

### NULL-значения

```go
// Используй стандартные типы Go для nullable DB полей
sql.NullString    // вместо *string
sql.NullInt64     // вместо *int64
sql.NullTime      // вместо *time.Time
pqtype.NullRawMessage // для JSONB
```

### Конвертеры

Сложные преобразования DB → API вынеси в `cmd/internal/server/converters.go`:

```go
func new<Entity>Response(e db.<Entity>, logger logging.Logger) <Entity>Response {
    return <Entity>Response{
        ID:            e.ID,
        KeyParameters: parseKeyParameters(e.KeyParameters, logger),
    }
}
```

### Временны́е метки

```go
// В response всегда форматируй время в RFC3339 или кастомный формат проекта
CreatedAt: e.CreatedAt.Format(time.RFC3339),
```

---

# Logging Standards

```go
// Контекстный логгер на каждый метод
logger := s.logger.WithField("method", "MyMethod")

// Форматы:
logger.Infof("processing tender id=%d", tenderID)
logger.WithError(err).Errorf("failed to create lot for tender=%d", tenderID)
logger.Warnf("invalid JSON in key_parameters for lot=%d, using empty object", lotID)

// Не логируй sensitive data (пароли, токены)
// Не логируй каждый SELECT — только важные события и ошибки
```

---

# Common Pitfalls (Don't Repeat)

| Проблема | Правило |
|----------|---------|
| Бизнес-логика в хендлере | Перенести в сервис |
| HTTP-зависимость в сервисе | Убрать, передавать только domain-данные |
| Raw SQL в коде | Только SQLC query files |
| `time.Now()` в тестируемом коде | Передавать `clock` или делать инъекцию |
| Дублирование конвертеров | Вынести в `converters.go` |
| Добавление поля без миграции | Сначала миграция, потом SQLC regenerate |
| `store.ExecTx` без отката ошибки | Всегда `return err` внутри транзакции |
| Конкатенация строк в SQL | Только параметры SQLC |
| Игнорирование ошибки регистрации маршрута | `gin` Routes не возвращают `error`, но дубли маршрутов вызывают panic |

---

# Devlog: Development Diary

После каждой значимой реализации (новый endpoint, исправление бага, архитектурное решение) создавай запись в `docs/devlog/`.

## Когда писать devlog

Писать **обязательно** при:
- Добавлении нового endpoint или сервиса.
- Исправлении нетривиального бага (особенно с архитектурным выводом).
- Изменении схемы БД (миграция + причина).
- Рефакторинге, затрагивающем несколько слоёв.
- Любом решении с trade-off (почему выбрано именно так).

Писать **не нужно** при:
- Тривиальных правках (опечатки, форматирование).
- Изменениях, уже описанных в PR-описании без архитектурной ценности.

## Имя файла

```
docs/devlog/YYYY-MM-DD_<kebab-case-slug>.md
```

Примеры:
```
2026-03-11_catalog-groups-api.md
2026-03-08_fix-create-simple-catalog-position-description.md
2026-03-03_reject-merge-endpoint.md
```

- Дата = дата реализации (сегодня).
- Слаг — короткое описание на английском в `kebab-case`.
- Если за день несколько записей — используй разные слаги (не нумерацию).

## Структура записи

```markdown
# YYYY-MM-DD — <Заголовок на русском: что было сделано>

## Контекст

Почему это понадобилось. Что было до этого изменения. Ссылки на предыдущие devlog-записи
если есть зависимость (например: "после devlog 2026-03-06 появился kind = 'GROUP_TITLE'").

## Проблема (опционально — только для bugfix)

Точное описание бага: что происходило, почему это неправильно, корневая причина.

## Решение (опционально — только для bugfix)

Как именно исправлено. Можно с кодовыми блоками ключевых изменений.

## Что сделано

Структурированный список по слоям. Каждый слой — отдельный подраздел ### N.

Примеры подразделов:
- ### 1. SQL-запросы (`cmd/internal/db/query/<entity>.sql`)
- ### 2. SQLC regenerate
- ### 3. Service layer (`cmd/internal/services/<domain>/<service>.go`)
- ### 4. HTTP Handler (`cmd/internal/server/handlers_<domain>.go`)
- ### 5. Регистрация маршрута (`cmd/internal/server/server.go`)

Для каждого изменения — краткое описание + ключевые фрагменты кода в блоках ```.

## Файлы затронуты

Финальный список всех изменённых/созданных файлов:

- `path/to/file.go` — краткое описание что изменено
- `path/to/another.go` — краткое описание

## Затронутые сценарии (опционально)

Какие бизнес-сценарии или API-endpoints затрагивает изменение (полезно для QA).

## Совместимость (опционально)

Обратная совместимость API, сигнатуры функций, схемы БД. Что НЕ изменилось.
```

## Правила написания

- **Язык**: русский (заголовки, описания). Код и имена файлов — как есть.
- **Аудитория**: будущий ты через 3 месяца. Объясняй "почему", а не только "что".
- **Кодовые блоки**: приводи только ключевые фрагменты, не весь файл.
- **Ссылки на другие devlog**: явно указывай зависимости (`devlog YYYY-MM-DD`).
- **Не дублируй git log**: devlog фиксирует архитектурные решения, а не diff.

---

# Testing Checklist: TESTING_CHECKLIST.md

После каждой задачи обновляй `TESTING_CHECKLIST.md` в корне проекта.

## Когда обновлять

- Написаны новые тесты → отметь `[x]` у соответствующих пунктов.
- Добавлена новая функция → добавь новые `[ ]` пункты в соответствующую фазу/раздел.
- Получен итоговый результат (количество тестов, покрытие) → запиши в `**Результат: ...**`.

## Структура записи

Чеклист организован по **Фазам** (`## Фаза N: <название>`) и **Задачам** (`### Задача N.M: <название>`).

Новые тесты для существующего сервиса добавляй в раздел этого сервиса:

```markdown
#### <Новый метод> (<ServiceName>) — unit-тесты

**Новые тесты:**
- [ ] Тест <MethodName> — <сценарий> (<ожидаемый результат>)
- [ ] Тест <MethodName> — ошибка БД <QueryName> (wrapped DB error)
- [ ] Тест <MethodName> — пустой <required_field> (ValidationError)
- [ ] **Результат:** (заполнить после написания тестов)
```

## Формат строки теста

```
- [x] Тест <MethodName> — <описание сценария> (<ожидаемое поведение>)
```

Примеры из проекта:
```markdown
- [x] Тест RejectMerge — статус не PENDING (APPROVED/REJECTED/EXECUTED → ValidationError)
- [x] Тест ExecuteMerge Сценарий 2 — ошибка CreateSimpleCatalogPosition (wrapped DB error)
- [x] Тест ListPendingMerges — пагинация (page=2, page_size=10 → offset=10)
```

## Строка Результат

После прохождения всех тестов в разделе добавь/обнови строку:

```markdown
- [x] **Результат: <N> unit тестов, все проходят. Покрытие: <что покрыто>.**
```

## Разделение по типу теста

- **Unit-тесты** → раздел под `### Задача 2.N: Тесты для <Service>`.
- **Integration-тесты** → `## Фаза 3` (Auth flow, транзакции, реальная БД).
- **E2E-тесты** → `## Фаза 4+`.

Не смешивай unit и integration в одном разделе.

---

# Output Format

По итогам реализации всегда предоставляй:

1. **Список изменённых/созданных файлов** по слоям (Migration → Query → SQLC → Service → Handler → server.go → app.go).
2. **Security checklist** — отмечены ли все пункты.
3. **Build validation** — вывод `go build ./...` и `go vet ./...`.
4. **Что нужно протестировать** — ссылка на skill `testing-go-app` с готовым списком BDD-сценариев.
5. **Devlog** — создай запись `docs/devlog/YYYY-MM-DD_<slug>.md` по шаблону из раздела выше.
6. **Testing Checklist** — добавь новые `[ ]` пункты в `TESTING_CHECKLIST.md` по шаблону из раздела выше.
