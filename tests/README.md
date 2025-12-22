# Тестовая инфраструктура

## Структура

```
cmd/internal/testutil/  # Утилиты для тестирования (используют internal пакеты)
├── db_helper.go      # Хелперы для работы с БД
├── fixtures.go       # Тестовые данные (использует db.sqlc типы)
├── assertions.go     # Кастомные проверки
└── test_server.go    # Утилиты для HTTP тестов

tests/
├── integration/      # Интеграционные тесты
├── e2e/             # End-to-end тесты
└── fixtures/        # Файлы с тестовыми данными (JSON, SQL)
```

> **Важно**: `testutil` находится в `cmd/internal/` чтобы иметь доступ к internal пакетам (использует `db.sqlc` типы напрямую, соблюдая DRY).

## Установленные зависимости

- **testify** v1.9.0 - Assertions и mocking
- **testcontainers-go** v0.28.0 - Docker контейнеры для тестов
- **go.uber.org/mock** v0.4.0 - Генерация моков
- **go-sqlmock** v1.5.2 - Мокирование SQL запросов

## Команды для запуска тестов

### Unit-тесты
```bash
make test-unit
```

### Интеграционные тесты
```bash
make test-integration
```

### E2E тесты
```bash
make test-e2e
```

### Все тесты
```bash
make test
```

### Отчет о покрытии
```bash
make test-coverage
```

### Watch mode (требуется entr)
```bash
# Установка entr: sudo apt install entr
make test-watch
```

## Утилиты

### DB Helper

Функции для работы с тестовой БД:

```go
// Создание контейнера PostgreSQL
db, container, err := testutil.SetupTestDatabase(t)
defer testutil.TeardownTestDatabase(t, db, container)

// Очистка таблиц между тестами
err := testutil.CleanupTables(t, db)

// Применение миграций
err := testutil.RunMigrations(t, db)
```

### Fixtures

Готовые тестовые данные:

```go
// Создание отдельных объектов
user := testutil.CreateTestUser("test@test.com", "Test User", false)
tender := testutil.CreateTestTender(1, "Test Tender", user.ID)

// Готовый набор данных
fixtures := testutil.DefaultFixtures()
// fixtures.Users, fixtures.Tenders, etc.

// Хелперы для nullable типов
description := testutil.String("Test description")
quantity := testutil.Float64(10.5)
```

### Assertions

Кастомные проверки:

```go
// Сравнение JSON
testutil.AssertJSONEqual(t, expectedJSON, actualJSON)

// Проверка ошибки
testutil.AssertErrorContains(t, err, "user not found")

// Обычные assertions (обертки над testify)
testutil.AssertEqual(t, expected, actual)
testutil.AssertNotEmpty(t, result)
testutil.AssertNil(t, err)
```

### Test Server

Утилиты для HTTP тестов:

```go
// Создание тестового сервера
server := testutil.NewTestServer()
server.Router.POST("/api/test", handler)

// Выполнение запросов
w := server.MakePostRequest(t, "/api/test", body, nil)

// С авторизацией
headers := testutil.WithAuth("jwt-token")
w := server.MakeGetRequest(t, "/api/users", headers)

// Проверка ответа
var response ResponseType
testutil.AssertResponse(t, w, http.StatusOK, &response)

// Проверка ошибки
testutil.AssertErrorResponse(t, w, http.StatusBadRequest, "invalid input")
```

## Примеры тестов

### Unit-тест

```go
package util

import (
    "testing"
    "github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

func TestHashPassword(t *testing.T) {
    password := "mysecretpassword"
    
    hash, err := HashPassword(password)
    
    testutil.AssertNoError(t, err)
    testutil.AssertNotEmpty(t, hash)
    testutil.AssertTrue(t, CheckPasswordHash(password, hash))
}
```

### Integration-тест с БД

```go
package integration

import (
    "testing"
    "github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
    _ "github.com/lib/pq"
)

func TestCreateUser(t *testing.T) {
    // Setup
    db, container, err := testutil.SetupTestDatabase(t)
    testutil.AssertNoError(t, err)
    defer testutil.TeardownTestDatabase(t, db, container)
    
    // Test
    user := testutil.CreateTestUser("test@test.com", "Test", false)
    // ... выполнить SQL запросы
    
    // Assert
    testutil.AssertNotEmpty(t, user.ID)
}
```

### Handler тест

```go
package server

import (
    "net/http"
    "testing"
    "github.com/zhukovvlad/tenders-go/cmd/internal/testutil"
)

func TestLoginHandler(t *testing.T) {
    // Setup
    server := testutil.NewTestServer()
    server.Router.POST("/login", LoginHandler)
    
    // Test
    body := map[string]string{
        "email": "test@test.com",
        "password": "password",
    }
    
    w := server.MakePostRequest(t, "/login", body, nil)
    
    // Assert
    var response LoginResponse
    testutil.AssertResponse(t, w, http.StatusOK, &response)
    testutil.AssertNotEmpty(t, response.Token)
}
```

## Best Practices

1. **Используйте t.Helper()** в утилитах для правильных номеров строк в ошибках
2. **Очищайте данные** между тестами (CleanupTables)
3. **Используйте фикстуры** для стандартных данных
4. **Параллелизация**: добавляйте `t.Parallel()` где возможно
5. **Timeout**: устанавливайте timeout для длинных тестов
6. **Мокирование**: мокируйте внешние зависимости (HTTP, сторонние API)
7. **Читаемость**: именуйте тесты описательно `TestFunctionName_Scenario_ExpectedResult`

## Требования

- Go 1.24+
- Docker (для integration и e2e тестов)
- PostgreSQL образ: `pgvector/pgvector:pg17`

## Следующие шаги

См. [TESTING_CHECKLIST.md](../TESTING_CHECKLIST.md) для полного плана внедрения тестов.
