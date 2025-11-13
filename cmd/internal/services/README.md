# Структура пакета Services

## Обзор

Этот пакет содержит бизнес-логику приложения, организованную в виде специализированных сервисов с использованием **паттерна композиции**.

## Структура директорий

```text
services/
├── apierrors/          # Кастомные типы ошибок для API
├── catalog/            # Операции управления каталогом
├── entities/           # CRUD операции с сущностями
├── importer/           # Основная оркестрация импорта тендеров
├── lot/                # Операции с лотами
└── matching/           # Логика сопоставления позиций
```

## Архитектурный паттерн: Композиция

### Почему композиция?

Кодовая база была рефакторена из монолитного `TenderImportService` со всеми методами в одном месте в **архитектуру на основе композиции**. Это было необходимо из-за ограничений языка Go:

1. **Go не позволяет добавлять методы к типам из других пакетов**
2. **Алиасы типов не решают проблему** - к ним также нельзя добавлять методы из внешних типов
3. **Композиция обеспечивает чёткое разделение ответственности** с сохранением инкапсуляции

### Как это работает

Сервисы **НЕ** скомпонованы друг в друга. Каждый сервис независим и создаётся в точке входа приложения (`app.go`), а затем передаётся туда, где он нужен через **Dependency Injection**:

```go
// В app.go:
entityManager := entities.NewEntityManager(logger)
tenderService := importer.NewTenderImportService(store, logger, entityManager)
catalogService := catalog.NewCatalogService(store, logger)
lotService := lot.NewLotService(store, logger)
matchingService := matching.NewMatchingService(store, logger)

server := server.NewServer(store, logger, tenderService, catalogService, lotService, matchingService, cfg)
```

**Важно:** `TenderImportService` знает ТОЛЬКО о `EntityManager`, потому что он нужен для импорта. Он НЕ знает о `Catalog`, `Lot` или `Matching` - эти сервисы используются в других местах (например, в HTTP handlers).

Каждый сервис:
- **Независимый** - может тестироваться и поддерживаться отдельно
- **Сфокусированный** - обрабатывает только свою предметную область
- **Получает зависимости извне** - следует принципу инверсии зависимостей (Dependency Injection)

### Паттерн использования

Сервисы используются напрямую в тех местах, где они нужны:

```go
// В handlers_rag.go:
response, err := s.catalogService.GetUnindexedCatalogItems(...)
err := s.matchingService.MatchPosition(...)

// В handlers_ai.go:
err := s.lotService.UpdateLotKeyParametersDirectly(...)

// В import_helpers.go (внутри TenderImportService):
err := s.Entities.GetOrCreateObject(...)
```

## Ответственность пакетов

### `entities/` - EntityManager
**Назначение**: Управление CRUD операциями с доменными сущностями

**Обязанности**:
- Создание или получение объектов, исполнителей, подрядчиков
- Управление позициями каталога и единицами измерения
- Обобщённые операции с сущностями через `getOrCreateOrUpdate`

**Ключевые методы**:
- `GetOrCreateObject`
- `GetOrCreateExecutor`
- `GetOrCreateContractor`
- `GetOrCreateCatalogPosition`
- `GetOrCreateUnitOfMeasurement`

### `catalog/` - CatalogService
**Назначение**: Обработка индексации и управления каталогом

**Обязанности**:
- Получение непроиндексированных элементов каталога
- Отметка элементов как активных/проиндексированных
- Предложение и управление слияниями каталога
- Запросы активных элементов каталога

**Ключевые методы**:
- `GetUnindexedCatalogItems`
- `MarkCatalogItemsAsActive`
- `SuggestMerge`
- `GetAllActiveCatalogItems`

### `lot/` - LotService
**Назначение**: Управление операциями с лотами

**Обязанности**:
- Обновление ключевых параметров лота
- Обработка изменений метаданных лота

**Ключевые методы**:
- `UpdateLotKeyParameters`
- `UpdateLotKeyParametersDirectly`

### `matching/` - MatchingService
**Назначение**: Обработка логики сопоставления позиций

**Обязанности**:
- Получение несопоставленных позиций
- Выполнение сопоставления позиций
- Валидация ограничений сопоставления

**Ключевые методы**:
- `GetUnmatchedPositions` (максимум 1000 позиций)
- `MatchPosition`

**Константы**:
- `MaxUnmatchedPositionsLimit = 1000`

### `importer/` - TenderImportService
**Назначение**: Оркестрация полного процесса импорта тендеров

**Обязанности**:
- Координация импорта тендеров
- Предоставление высокоуровневых рабочих процессов импорта
- Управление границами транзакций
- Обработка специфичной бизнес-логики импорта

**Зависимости**:
- Использует **ТОЛЬКО** `EntityManager` для операций с сущностями
- **НЕ** зависит от `CatalogService`, `LotService` или `MatchingService`

**Важно:** Сервис импорта не должен знать о сервисах RAG-воркеров (catalog, matching) или точечных обновлений (lot). Это обеспечивает правильное разделение ответственности.

### `apierrors/` - Кастомные ошибки
**Назначение**: Типизированная обработка ошибок для API слоя

**Типы**:
- `ValidationError` - для ошибок валидации входных данных (HTTP 400 Bad Request)
- `NotFoundError` - для ошибок "ресурс не найден" (HTTP 404 Not Found)

**Использование в handlers**:
```go
var notFoundErr *apierrors.NotFoundError
var validationErr *apierrors.ValidationError

if errors.As(err, &notFoundErr) {
    c.JSON(http.StatusNotFound, errorResponse(err))
} else if errors.As(err, &validationErr) {
    c.JSON(http.StatusBadRequest, errorResponse(err))
} else {
    c.JSON(http.StatusInternalServerError, errorResponse(err))
}
```

## Преимущества этой структуры

1. **Разделение ответственности**: Каждый сервис имеет единственную, чётко определённую обязанность
2. **Тестируемость**: Сервисы могут быть протестированы независимо с мок-зависимостями
3. **Поддерживаемость**: Изменения в одной области не влияют на другие
4. **Переиспользуемость**: Сервисы могут использоваться в разных хендлерах и рабочих процессах
5. **Явные зависимости**: Dependency Injection делает зависимости явными и видимыми
6. **Go-идиоматичность**: Следует предпочтению Go композиции над наследованием
7. **Избегание "Божественного объекта"**: Ни один сервис не знает обо всех остальных - каждый получает только то, что ему нужно

## Принципы архитектуры

### 1. Dependency Injection (Внедрение зависимостей)
Все сервисы получают свои зависимости через конструктор, а не создают их внутри:

```go
// ✅ ПРАВИЛЬНО - зависимости передаются извне
func NewTenderImportService(
    store db.Store,
    logger *logging.Logger,
    entityManager *entities.EntityManager,  // Получаем как аргумент
) *TenderImportService {
    return &TenderImportService{
        store:    store,
        logger:   logger,
        Entities: entityManager,  // Просто сохраняем
    }
}

// ❌ НЕПРАВИЛЬНО - создание зависимостей внутри
func NewTenderImportService(store db.Store, logger *logging.Logger) *TenderImportService {
    return &TenderImportService{
        store:    store,
        logger:   logger,
        Entities: entities.NewEntityManager(logger),  // Создаём внутри - плохо!
    }
}
```

### 2. Минимальные зависимости
Каждый сервис знает только о том, что ему действительно нужно:

- `TenderImportService` → знает только о `EntityManager`
- `CatalogService` → знает только о `store` и `logger`
- `LotService` → знает только о `store` и `logger`
- `MatchingService` → знает только о `store` и `logger`

### 3. Создание в точке входа
Все сервисы создаются в `app.go` и передаются туда, где нужны:

```go
// app.go - единая точка создания всех сервисов
entityManager := entities.NewEntityManager(logger)
tenderService := importer.NewTenderImportService(store, logger, entityManager)
catalogService := catalog.NewCatalogService(store, logger)
lotService := lot.NewLotService(store, logger)
matchingService := matching.NewMatchingService(store, logger)

// Сервер получает те сервисы, которые ему нужны
server := server.NewServer(store, logger, tenderService, catalogService, lotService, matchingService, cfg)
```

## Заметки по миграции

При рефакторинге из старой структуры:

1. **Переместите методы в соответствующие пакеты сервисов** на основе их предметной области
2. **Используйте Dependency Injection** - передавайте зависимости через конструктор, не создавайте их внутри
3. **Минимизируйте зависимости** - каждый сервис должен знать только о том, что ему нужно
4. **Создавайте сервисы в app.go** - единая точка создания и управления зависимостями
5. **Импортируйте пакеты корректно** - используйте полные пути импорта для сервисов

Пример миграции:
```go
// ДО: Монолитный сервис
type TenderImportService struct {
    store  db.Store
    logger *logging.Logger
}

func (s *TenderImportService) GetOrCreateObject(...) { }
func (s *TenderImportService) MatchPosition(...) { }
func (s *TenderImportService) UpdateLotKeyParameters(...) { }

// ПОСЛЕ: Разделённые сервисы

// entities/manager.go:
type EntityManager struct {
    logger *logging.Logger
}
func (m *EntityManager) GetOrCreateObject(...) { }

// matching/matching_service.go:
type MatchingService struct {
    store  db.Store
    logger *logging.Logger
}
func (m *MatchingService) MatchPosition(...) { }

// lot/lot_service.go:
type LotService struct {
    store  db.Store
    logger *logging.Logger
}
func (l *LotService) UpdateLotKeyParameters(...) { }

// importer/import_service.go:
type TenderImportService struct {
    store    db.Store
    logger   *logging.Logger
    Entities *entities.EntityManager  // Только то, что нужно для импорта
}

// app.go - создание и связывание:
entityManager := entities.NewEntityManager(logger)
tenderService := importer.NewTenderImportService(store, logger, entityManager)
matchingService := matching.NewMatchingService(store, logger)
lotService := lot.NewLotService(store, logger)

// Использование в разных местах:
// В import_helpers.go:
err := s.Entities.GetOrCreateObject(...)

// В handlers_rag.go:
err := s.matchingService.MatchPosition(...)

// В handlers_ai.go:
err := s.lotService.UpdateLotKeyParameters(...)
```

## Анти-паттерны, которых нужно избегать

### ❌ "Божественный объект" (God Object)
```go
// ПЛОХО - один сервис знает обо всех остальных
type TenderImportService struct {
    Entities *entities.EntityManager
    Catalog  *catalog.CatalogService    // Не нужен для импорта!
    Lot      *lot.LotService            // Не нужен для импорта!
    Matching *matching.MatchingService  // Не нужен для импорта!
}
```

### ❌ Создание зависимостей внутри конструктора
```go
// ПЛОХО - скрытые зависимости
func NewTenderImportService(store, logger) *TenderImportService {
    return &TenderImportService{
        Entities: entities.NewEntityManager(logger),  // Создаём внутри!
    }
}
```

### ✅ Правильный подход
```go
// ХОРОШО - явные зависимости через параметры
func NewTenderImportService(
    store db.Store,
    logger *logging.Logger,
    entityManager *entities.EntityManager,  // Получаем извне
) *TenderImportService {
    return &TenderImportService{
        store:    store,
        logger:   logger,
        Entities: entityManager,  // Просто сохраняем
    }
}
```
