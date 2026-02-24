# 📋 Чек-лист внедрения системы тестирования

## Фаза 0: Подготовка инфраструктуры

### ✅ Задача 0.1: Установка зависимостей
- [x] Добавить `github.com/stretchr/testify v1.11.1` в go.mod
- [x] Добавить `github.com/testcontainers/testcontainers-go v0.40.0` в go.mod
- [x] Добавить `go.uber.org/mock v0.6.0` в go.mod
- [x] Добавить `github.com/DATA-DOG/go-sqlmock v1.5.2` в go.mod
- [x] Выполнить `go mod tidy`

### Задача 0.2: Создание директорий для тестов
- [x] Создать `cmd/internal/testutil/` для тестовых утилит (доступ к internal)
- [ ] Создать `tests/integration/` для интеграционных тестов
- [ ] Создать `tests/e2e/` для end-to-end тестов
- [ ] Создать `tests/fixtures/` для тестовых данных

### ✅ Задача 0.3: Настройка Makefile
- [x] Добавить команду `make test` (запуск всех тестов)
- [x] Добавить команду `make test-unit` (только unit-тесты)
- [x] Добавить команду `make test-integration` (интеграционные тесты)
- [x] Добавить команду `make test-e2e` (e2e тесты)
- [x] Добавить команду `make test-coverage` (с отчетом покрытия)
- [x] Добавить команду `make test-watch` (watch mode для разработки)
- [x] `make sqlc` автоматически перегенерирует mockgen-моки (mock_querier.go, mock_store.go) после sqlc generate

### ✅ Задача 0.4: Создание тестовых утилит
- [x] Создать `cmd/internal/testutil/db_helper.go` (хелперы для БД)
- [x] Создать `cmd/internal/testutil/fixtures.go` (фикстуры, использует db.sqlc типы - DRY!)
- [x] Создать `cmd/internal/testutil/assertions.go` (кастомные проверки)
- [x] Создать `cmd/internal/testutil/mock_logger.go` (мок логгера для тестов)
- [x] Создать `cmd/internal/testutil/test_server.go` (тестовый HTTP сервер)
- [x] Создать `tests/README.md` (документация тестирования)
- [x] Добавить shared-хелперы в `assertions.go`: `FindResponseCookie`, `AssertNoTokensInBody`, `AssertAuthCookieSecurity`

---

## Фаза 1: Unit-тесты для утилит (Простые тесты для начала)

### ✅ Задача 1.1: Тесты для hash_utils.go
- [x] Создать `cmd/internal/util/hash_utils_test.go`
- [x] Тест `TestHashPassword` (проверка хеширования)
- [x] Тест `TestCheckPasswordHash` (проверка сравнения паролей)
- [x] Тест `TestCheckPasswordHash_WrongPassword` (негативный кейс)
- [x] Тест `TestHashPassword_EmptyPassword` (граничный случай)

### ✅ Задача 1.2: Тесты для nullable.go
- [x] Создать `cmd/internal/util/nullable_test.go`
- [x] Тесты для всех функций работы с nullable типами
- [x] Проверка граничных случаев (nil, empty string, zero values)

### ✅ Задача 1.3: Запуск и проверка
- [x] Выполнить `make test-unit`
- [x] Убедиться, что все тесты проходят
- [x] Проверить покрытие: `go test -cover ./cmd/internal/util/...`
- [x] **Результат: Покрытие 97.8%**

---

## Фаза 2: Unit-тесты для сервисов

### ✅ Задача 2.1: Тесты для Auth Service
- [x] Создать `cmd/internal/services/auth/auth_service_test.go`
- [x] Создать моки для `db.Store` с помощью gomock или интерфейса
- [x] Тест `TestGenerateAccessToken_Success` (генерация JWT токенов)
- [x] Тест `TestValidateAccessToken_Success` (валидация токенов)
- [x] Тест `TestValidateAccessToken_Expired` (истекшие токены)
- [x] Тест `TestValidateAccessToken_WrongSignature` (неверная подпись)
- [x] Тест `TestValidateAccessToken_Malformed` (некорректный формат)
- [x] Тест `TestValidateAccessToken_UnsafeAlgorithm` (защита от алгоритма "none")
- [x] Тест `TestGenerateRefreshToken_Format` (формат refresh токенов)
- [x] Тест `TestGenerateRefreshToken_Uniqueness` (уникальность токенов)
- [x] Тест `TestValidateRefreshTokenFormat_Valid` (валидация формата)
- [x] Тест `TestValidateRefreshTokenFormat_Invalid` (некорректные форматы)
- [x] Тест `TestValidateUserAgent_TruncatesLong` (обрезка длинных User-Agent)
- [x] Тест `TestValidateUserAgent_UTF8Safe` (безопасная работа с UTF-8)
- [x] Тесты для helper функций (hashIdentifier, hashUserID, ipToInet)
- [x] Добавлен sentinel error `ErrTokenExpired` — отдельная ошибка для истекших JWT (отличается от `ErrInvalidToken`)
- [x] Тест `TestErrorConstants` обновлён: проверка уникальности и содержимого `ErrTokenExpired`
- [x] **Результат: 24 unit теста, все проходят. Покрытие token/validation логики: ~95%**
- [x] **NOTE: Login/Refresh/Logout требуют транзакций и будут протестированы в integration тестах (Phase 3)**

### ✅ Задача 2.2: Тесты для Catalog Service
- [x] Создать `cmd/internal/services/catalog/catalog_service_test.go`
- [x] Мок для database queries (gomock MockStore, регенерирован для полноты интерфейса)
- [x] Введён Logger interface для тестируемости (по аналогии с auth service)
- [x] Тесты GetUnindexedCatalogItems (получение неиндексированных позиций)
- [x] Тесты GetAllActiveCatalogItems (получение активных позиций с пагинацией)
- [x] Тесты MarkCatalogItemsAsActive (обновление статуса позиций)
- [x] Тесты SuggestMerge (предложение слияния дубликатов)
- [x] Тесты buildContextString (приоритет описания над лемматизированным названием)
- [x] Тесты валидации параметров (negative limit/offset → ValidationError)
- [x] Тесты обработки ошибок БД (wrapped errors)
- [x] Тесты граничных случаев (пустые списки, nil, self-merge)
- [ ] Тесты ExecuteMerge — успешное выполнение одобренного слияния (транзакция: MergeCatalogPosition + UpdateMergeStatus)
- [ ] Тесты ExecuteMerge — предложение не найдено (NotFoundError)
- [ ] Тесты ExecuteMerge — статус не APPROVED (ValidationError)
- [ ] Тесты ExecuteMerge — дубликат уже влит (повторное слияние → ValidationError)
- [ ] Тесты ExecuteMerge — ошибка БД при MergeCatalogPosition / UpdateMergeStatus (rollback)
- [ ] Тесты GetMergedPositions — получение всех влитых позиций для мастера
- [x] **Результат: 24 unit теста, все проходят. TODO: +6 тестов для ExecuteMerge/GetMergedPositions**

### ✅ Задача 2.3: Тесты для Lot Service
- [x] Создать `cmd/internal/services/lot/lot_service_test.go`
- [x] Введён Logger interface с поддержкой WithField/WithFields для тестируемости (по аналогии с auth/catalog)
- [x] Создан logrusAdapter для production-кода, обновлён `cmd/main/app.go`
- [x] Мок ExecTx через go-sqlmock: sqlmock-backed *Queries внутри DoAndReturn для тестирования транзакционной логики
- [x] Тесты UpdateLotKeyParameters — успешное обновление через tender_etp_id + lot_key
- [x] Тесты UpdateLotKeyParameters — тендер не найден (NotFoundError)
- [x] Тесты UpdateLotKeyParameters — лот не найден (NotFoundError)
- [x] Тесты UpdateLotKeyParameters — ошибки БД при поиске тендера/лота/обновлении (wrapped errors)
- [x] Тесты UpdateLotKeyParameters — ошибка сериализации JSON (до ExecTx)
- [x] Тесты UpdateLotKeyParameters — ошибка ExecTx (tx begin failed)
- [x] Тесты UpdateLotKeyParameters — пустые параметры (корректный JSON: `{}`)
- [x] Тесты UpdateLotKeyParametersDirectly — успешное обновление по DB ID
- [x] Тесты UpdateLotKeyParametersDirectly — невалидные lot_id: строка, пустое, float, overflow int64 (ValidationError)
- [x] Тесты UpdateLotKeyParametersDirectly — лот не найден (NotFoundError)
- [x] Тесты UpdateLotKeyParametersDirectly — ошибки БД при поиске/обновлении (wrapped errors)
- [x] Тесты UpdateLotKeyParametersDirectly — граничные значения (max int64, отрицательный ID)
- [x] Тесты NewLotService (конструктор)
- [x] Тесты соответствия интерфейсу Logger (mockLogger, logrusAdapter)
- [x] **Результат: 24 unit теста, все проходят. Добавлена зависимость go-sqlmock для тестирования ExecTx**

### ✅ Задача 2.4: Тесты для Matching Service
- [x] Создать `cmd/internal/services/matching/matching_service_test.go`
- [x] Мок для database queries (gomock MockStore, go-sqlmock для *Queries внутри ExecTx)
- [x] Тесты GetUnmatchedPositions — успешное получение позиций с breadcrumbs (rich_context_string)
- [x] Тесты GetUnmatchedPositions — позиции без breadcrumbs (root positions → "Позиция: ...")
- [x] Тесты GetUnmatchedPositions — позиции с draft_catalog_id (pending_indexing)
- [x] Тесты GetUnmatchedPositions — множественные позиции (mixed: breadcrumbs + root + draft)
- [x] Тесты GetUnmatchedPositions — пустой результат (empty slice, not nil)
- [x] Тесты GetUnmatchedPositions — валидация limit (zero, negative, min int32 → ValidationError)
- [x] Тесты GetUnmatchedPositions — capping limit до MaxUnmatchedPositionsLimit (1000)
- [x] Тесты GetUnmatchedPositions — граничные значения limit (just below, exactly at, just above max)
- [x] Тесты GetUnmatchedPositions — ошибка БД (wrapped error)
- [x] Тесты GetUnmatchedPositions — table-driven: построение context string (single/nested breadcrumbs, root)
- [x] Тесты MatchPosition — успешное сопоставление (SetCatalogPositionID + UpsertMatchingCache в транзакции)
- [x] Тесты MatchPosition — norm_version по умолчанию (0 → 1)
- [x] Тесты MatchPosition — явный norm_version (table-driven: 0→1, 1, 2, 5)
- [x] Тесты MatchPosition — ошибка SetCatalogPositionID (deadlock → wrapped error)
- [x] Тесты MatchPosition — GetPositionItemByID fails (non-critical, continues с пустым job_title_text)
- [x] Тесты MatchPosition — ошибка UpsertMatchingCache (constraint violation → wrapped error)
- [x] Тесты MatchPosition — ошибка ExecTx (tx begin failed)
- [x] Тесты MatchPosition — позиция с пустым job_title (NullString{Valid: false})
- [x] Тесты NewMatchingService (конструктор)
- [x] Тест MaxUnmatchedPositionsLimit константа (документированный контракт = 1000)
- [x] **Результат: 29 unit тестов (включая sub-tests), все проходят. Покрытие: GetUnmatchedPositions + MatchPosition + валидация + транзакции + edge cases**

### ✅ Задача 2.5: Тесты для Importer Service (Выполнено)
- [x] Создать `cmd/internal/services/importer/import_service_test.go`
- [x] Мок Store (gomock MockStore) + sqlmock для ExecTx callback
- [x] Тест успешного импорта тендера без лотов
- [x] Тест успешного импорта тендера с 1 лотом (baseline + позиции + итоги)
- [x] Тест matching_cache hit (кэшированная позиция каталога)
- [x] Тест contractor proposal с additional_info (delete + upsert)
- [x] Тест обработки ошибок: ExecTx fails, Object/Executor/Tender/Lot/Proposal/PositionItem/RawData creation fails
- [x] Тест ошибок: Unit creation fails, CatalogPosition creation fails, MatchingCache DB error, Summary line fails, AdditionalInfo delete fails
- [x] Тест HEADER позиции (пропуск matching_cache)
- [x] Тесты чистых маппер-функций: mapApiPositionToDbParams, mapApiSummaryToDbParams (заполненные + nil поля)
- [x] Edge cases: пустой job_title, nil Positions/Summary, повторное использование существующих сущностей
- [x] **Результат: 25 unit тестов, все проходят. Покрытие: ImportFullTender pipeline + error propagation + pure mappers + edge cases**

---

## Фаза 3: Unit-тесты для Converters

### ✅ Задача 3.1: Тесты для converters.go
- [x] Создать `cmd/internal/server/converters_test.go`
- [x] Тесты parseKeyParameters: Valid JSON, NULL, empty bytes, "null" string, invalid JSON + warning log
- [x] Тесты parseKeyParameters: nested JSON, array JSON, boolean/number types preservation, empty object
- [x] Тесты parseKeyParameters: Valid=false with data (DB NULL), whitespace-only JSON, Unicode JSON
- [x] Тесты newLotResponse: все поля корректно маппятся (ID, LotKey, LotTitle, TenderID, KeyParameters)
- [x] Тесты newLotResponse: timestamps форматируются в RFC3339 (UTC, non-UTC timezone — table-driven)
- [x] Тесты newLotResponse: Proposals/Winners инициализируются пустыми слайсами (не nil)
- [x] Тесты newLotResponse: JSON-сериализация → `"proposals":[]` и `"winners":[]` (не null)
- [x] Тесты newLotResponse: fallback на `{}` при невалидных key_parameters + warning log
- [x] Тесты newLotResponse: zero-value db.Lot (граничный случай)
- [x] Тесты newLotResponse: полный маппинг полей с разными CreatedAt/UpdatedAt
- [x] **Результат: 19 тестов (22 включая sub-tests), все проходят. Покрытие converters.go: 100%**

---

## Фаза 4: Integration-тесты для Database Layer

### ✅ Задача 4.1: Настройка testcontainers (Выполнено в testutil)
- [x] Создать `cmd/internal/testutil/db_helper.go` (реализовано вместо `db_setup_test.go`)
- [x] Функция создания PostgreSQL контейнера (`SetupTestDatabase`)
- [x] Функция применения миграций к тестовой БД (`RunMigrations`)
- [x] Функция очистки данных между тестами (`CleanupTables`)
- [x] Функция teardown контейнера (`TeardownTestDatabase`)

### Задача 4.2: Тесты SQLC queries
- [ ] Создать `tests/integration/db_users_test.go`
- [ ] Тесты для user-related queries
- [ ] Тест `CreateUser` с реальной БД
- [ ] Тест `GetUserByEmail`
- [ ] Тест `UpdateUser`
- [ ] Тест `DeleteUser`

### Задача 4.3: Тесты для tender queries
- [ ] Создать `tests/integration/db_tenders_test.go`
- [ ] Тесты CRUD операций для тендеров
- [ ] Тесты сложных запросов с JOIN
- [ ] Тесты транзакций

### Задача 4.4: Тесты для lot queries
- [ ] Создать `tests/integration/db_lots_test.go`
- [ ] Тесты для лотов и связанных сущностей
- [ ] Тесты каскадных операций

### Задача 4.5: Тесты миграций
- [ ] Создать `tests/integration/migrations_test.go`
- [ ] Тест применения миграций с нуля
- [ ] Тест отката миграций
- [ ] Тест идемпотентности миграций
- [ ] Тест миграции 000003: merged_into_id (BIGINT, FK RESTRICT, CHECK self-merge, индекс)

### Задача 4.6: Тесты ограничений целостности (из TODO.md)
- [ ] Тест `ON DELETE RESTRICT` для тендеров (наличие лотов)
- [ ] Тест `ON DELETE RESTRICT` для подрядчиков (наличие персон)
- [ ] Тест `ON DELETE CASCADE` для типов тендеров
- [ ] Тест `ON DELETE CASCADE` для лотов

---

## Фаза 5: Integration-тесты для API Handlers

### ✅ Задача 5.1: Подготовка тестового окружения (Выполнено в testutil)
- [x] Создать `cmd/internal/testutil/test_server.go`
- [x] Функция создания тестового Gin роутера (`NewTestServer`)
- [x] Хелперы для HTTP запросов (GET, POST, PUT, DELETE)
- [x] Хелперы для проверки JSON ответов (`AssertResponse`)

### ✅ Задача 5.2: Тесты для handlers_auth.go
- [x] Создать `cmd/internal/server/handlers_auth_test.go`
- [x] Тест `POST /api/auth/login` (успех) — TestLoginHandler_Success
- [x] Тест `POST /api/auth/login` (валидация) — TestLoginHandler_ValidationErrors (5 sub-tests), TestLoginHandler_MalformedJSON
- [x] Тест `POST /api/auth/login` (неверные credentials) — TestLoginHandler_WrongPassword, TestLoginHandler_UserNotFound, TestLoginHandler_InactiveUser
- [x] Тест `POST /api/auth/login` (ошибки БД) — TestLoginHandler_DBError, TestLoginHandler_SessionCreationFailed
- [x] Тест `POST /api/auth/refresh` (успех) — TestRefreshHandler_Success
- [x] Тест `POST /api/auth/refresh` (ошибки) — TestRefreshHandler_NoCookie, TestRefreshHandler_InvalidTokenFormat, TestRefreshHandler_SessionNotFound, TestRefreshHandler_DBError
- [x] Тест `POST /api/auth/logout` (успех) — TestLogoutHandler_Success, TestLogoutHandler_NoCookie
- [x] Тест `POST /api/auth/logout` (CSRF) — TestLogoutHandler_MissingCSRF, TestLogoutHandler_CSRFMismatch
- [x] Тест `POST /api/auth/logout` (ошибки) — TestLogoutHandler_ServiceError
- [x] Тест `GET /api/auth/me` — TestMeHandler_Success, TestMeHandler_NoAuth, TestMeHandler_InvalidToken, TestMeHandler_DBError
- [x] **Безопасность (XSS/cookie)**:
  - TestLoginHandler_TokensNotInResponseBody — JWT/refresh-токены не утекают в JSON-body
  - TestLoginHandler_CookieSecurityAttributes — HttpOnly, Secure, SameSite=Strict, Path=/
  - TestLoginHandler_EmailNormalization — нормализация email (trim + lowercase)
- [x] **Протухшие и поддельные токены**:
  - TestMeHandler_ExpiredToken — истекший JWT → 401 + X-Auth-Error: `access_token_expired` + cookie cleared
  - TestMeHandler_WrongSigningKey — чужой signing key → 401 + X-Auth-Error: `access_token_invalid` + cookie cleared
- [x] **Транзакционная целостность refresh**:
  - TestRefreshHandler_RevokeOldSessionFails — откат при ошибке revoke
  - TestRefreshHandler_CreateNewSessionFails — откат при ошибке create session
  - TestRefreshHandler_GetUserFailsInsideTx — откат при ошибке get user внутри tx
- [x] **Defense-in-depth**:
  - TestRefreshHandler_ExpiredSessionTimeMismatch — Go-side time check ловит expired session при рассинхроне DB/app
  - TestRefreshHandler_CookiesClearedOnAuthError — куки очищены при auth-ошибке refresh
  - TestRefreshHandler_CookiesNotClearedOnInternalError — куки НЕ очищены при internal error
  - TestRefreshHandler_CookieSecurityAttributes — HttpOnly, Secure, SameSite=Strict для refresh cookies
- [x] **Edge cases**:
  - TestLogoutHandler_InvalidRefreshTokenFormat — logout с невалидным форматом refresh token
  - TestCSRF_HeaderMissingCookiePresent — CSRF: X-CSRF-Token header есть, cookie нет → rejected
- [x] **Middleware**: X-Auth-Error различает `access_token_expired` (истёк) и `access_token_invalid` (подделан/повреждён)
- [x] **Shared-хелперы**: `performSuccessfulLogin` (DRY для login mock setup), `testutil.FindResponseCookie`, `testutil.AssertNoTokensInBody`, `testutil.AssertAuthCookieSecurity`
- [x] **NOTE: `POST /api/auth/register` не реализован — создание пользователей через CLI (cmd/createadmin) и admin API**
- [x] **Результат: 38 тестов (включая sub-tests), все проходят. Покрытие: login, refresh, logout, me + CSRF + AuthMiddleware + security + transaction integrity**

### Задача 5.3: Тесты для handlers_tender.go
- [ ] Создать `cmd/internal/server/handlers_tender_test.go`
- [ ] Тест `GET /api/tenders` (список)
- [ ] Тест `GET /api/tenders/:id` (получение)
- [ ] Тест `POST /api/tenders` (создание)
- [ ] Тест `PUT /api/tenders/:id` (обновление)
- [ ] Тест `DELETE /api/tenders/:id` (удаление)
- [ ] Тесты pagination и filtering

### Задача 5.4: Тесты для handlers_lots.go
- [ ] Создать `cmd/internal/server/handlers_lots_test.go`
- [ ] Аналогичные CRUD тесты для лотов
- [ ] Тесты связей лотов с тендерами

### Задача 5.5: Тесты для handlers_categories.go
- [ ] Создать `cmd/internal/server/handlers_categories_test.go`
- [ ] CRUD тесты для категорий

### Задача 5.6: Тесты для handlers_proposals.go
- [ ] Создать `cmd/internal/server/handlers_proposals_test.go`
- [ ] Тесты создания и управления предложениями

### Задача 5.7: Тесты для handlers_import.go
- [ ] Создать `cmd/internal/server/handlers_import_test.go`
- [ ] Тест импорта тендера
- [ ] Тест валидации данных импорта
- [ ] Тест обработки ошибок внешнего API

### Задача 5.8: Тесты для handlers_ai.go & handlers_rag.go
- [ ] Создать тесты для AI-эндпоинтов
- [ ] Мокирование AI сервисов
- [ ] Тесты ExecuteMergeHandler (`POST /api/v1/admin/merges/:id/execute`)
  - [ ] Успешное выполнение слияния (200 + ExecuteMergeResponse)
  - [ ] Невалидный ID (400)
  - [ ] Предложение не найдено (404)
  - [ ] Статус не APPROVED (400)
  - [ ] Ошибка БД (500)
  - [ ] Проверка требования роли admin

---

## Фаза 6: Тесты для Middleware

### Задача 6.1: Тесты для middleware_auth.go
- [ ] Создать `cmd/internal/server/middleware_auth_test.go`
- [ ] Тест с валидным токеном
- [ ] Тест без токена
- [ ] Тест с невалидным токеном
- [ ] Тест с истекшим токеном

### Задача 6.2: Тесты для middleware_csrf.go
- [ ] Создать `cmd/internal/server/middleware_csrf_test.go`
- [ ] Тест валидного CSRF токена
- [ ] Тест невалидного CSRF токена
- [ ] Тест отсутствия токена

### Задача 6.3: Тесты для middleware_rate_limit.go
- [ ] Создать `cmd/internal/server/middleware_rate_limit_test.go`
- [ ] Тест нормального использования (под лимитом)
- [ ] Тест превышения лимита
- [ ] Тест восстановления после timeout

### Задача 6.4: Тесты для middleware_service_auth.go
- [ ] Создать тесты для service-to-service authentication

---

## Фаза 7: E2E тесты (End-to-End)

### Задача 7.1: Инфраструктура E2E
- [ ] Создать `tests/e2e/setup_test.go`
- [ ] Настройка полного окружения (БД + сервер)
- [ ] Функция запуска полного приложения
- [ ] Cleanup после тестов

### Задача 7.2: E2E: Регистрация и авторизация
- [ ] Создать `tests/e2e/auth_flow_test.go`
- [ ] Сценарий: регистрация → логин → получение профиля
- [ ] Сценарий: логин → обновление профиля → logout

### Задача 7.3: E2E: Работа с тендерами
- [ ] Создать `tests/e2e/tender_flow_test.go`
- [ ] Сценарий: создание тендера → публикация → получение списка
- [ ] Сценарий: создание тендера → добавление лотов → импорт данных

### Задача 7.4: E2E: Работа с предложениями
- [ ] Создать `tests/e2e/proposal_flow_test.go`
- [ ] Сценарий: просмотр тендера → создание предложения → отправка
- [ ] Сценарий: управление статусами предложений

### Задача 7.5: E2E: Matching system
- [ ] Создать `tests/e2e/matching_flow_test.go`
- [ ] Сценарий: создание каталога → создание предложения → матчинг
- [ ] Проверка scoring и ранжирования

---

## Фаза 8: Настройка CI/CD

### Задача 8.1: GitHub Actions workflow
- [ ] Создать `.github/workflows/tests.yml`
- [ ] Job для unit-тестов
- [ ] Job для integration-тестов
- [ ] Job для e2e-тестов
- [ ] Матрица Go версий (1.23, 1.24)
- [ ] Кэширование зависимостей

### Задача 8.2: Покрытие кода
- [ ] Настроить генерацию coverage report
- [ ] Интеграция с codecov.io или coveralls
- [ ] Установить минимальный порог покрытия (70%)
- [ ] Badge с покрытием в README.md

### Задача 8.3: Линтеры и статический анализ
- [ ] Добавить golangci-lint в CI
- [ ] Создать `.golangci.yml` с настройками
- [ ] Добавить `go vet` в pipeline
- [ ] Добавить `go fmt` проверку

### Задача 8.4: Pre-commit hooks
- [ ] Установить pre-commit framework
- [ ] Хук для запуска unit-тестов
- [ ] Хук для форматирования кода
- [ ] Хук для линтинга

---

## Фаза 9: Документация и best practices

### Задача 9.1: Документация тестирования
- [ ] Создать `docs/TESTING.md`
- [ ] Описание структуры тестов
- [ ] Примеры написания тестов
- [ ] Best practices для проекта
- [ ] Как запускать тесты локально

### Задача 9.2: Test fixtures и данные
- [ ] Создать стандартные фикстуры в `tests/fixtures/`
- [ ] JSON файлы с тестовыми данными
- [ ] SQL скрипты для seed данных
- [ ] Документация использования fixtures

### Задача 9.3: Обновление README.md
- [ ] Добавить секцию "Testing"
- [ ] Команды для запуска тестов
- [ ] Требования для локальной разработки
- [ ] Badges (tests, coverage)

---

## Фаза 10: Оптимизация и поддержка

### Задача 10.1: Оптимизация тестов
- [ ] Анализ времени выполнения тестов
- [ ] Параллелизация медленных тестов (t.Parallel())
- [ ] Оптимизация использования testcontainers
- [ ] Кэширование подготовки данных

### Задача 10.2: Test utilities рефакторинг
- [ ] Убрать дублирование в тестах
- [ ] Создать переиспользуемые хелперы
- [ ] Стандартизировать assertions
- [ ] Улучшить читаемость тестов

### Задача 10.3: Continuous improvement
- [ ] Настроить мониторинг flaky tests
- [ ] Регулярный review покрытия
- [ ] Обновление зависимостей для тестов
- [ ] Документирование новых паттернов

---

## Метрики успеха

- [ ] ✅ Покрытие unit-тестами: **> 80%**
- [ ] ✅ Покрытие integration-тестами: **> 60%**
- [ ] ✅ E2E тесты для основных флоу: **100%**
- [ ] ✅ Все тесты проходят на CI: **✓**
- [ ] ✅ Время выполнения всех тестов: **< 5 минут**
- [ ] ✅ Zero flaky tests: **✓**

---

## Примерный timeline

- **Фаза 0-1**: 2-3 дня (инфраструктура + простые тесты)
- **Фаза 2-3**: 5-7 дней (unit-тесты сервисов)
- **Фаза 4**: 3-5 дней (integration БД)
- **Фаза 5-6**: 7-10 дней (API handlers + middleware)
- **Фаза 7**: 3-5 дней (E2E)
- **Фаза 8**: 2-3 дня (CI/CD)
- **Фаза 9-10**: 2-3 дня (документация + оптимизация)

**Итого**: ~4-6 недель для полного покрытия

---

## Приоритеты

### 🔴 Критично (сделать в первую очередь)
- Фаза 0: Инфраструктура
- Фаза 1: Утилиты (быстро, просто)
- Фаза 2.1: Auth Service (критичная бизнес-логика)
- Фаза 5.2: Auth handlers

### 🟡 Важно
- Фаза 2.2-2.5: Остальные сервисы
- Фаза 4: Integration БД
- Фаза 5: API handlers
- Фаза 8: CI/CD

### 🟢 Желательно
- Фаза 7: E2E тесты
- Фаза 9: Документация
- Фаза 10: Оптимизация

---

## Примечания

- После каждой фазы делать commit и push
- Писать тесты параллельно с фичами (TDD подход для новых фич)
- Регулярно запускать `make test-coverage` для контроля
- Не стремиться к 100% покрытию - фокус на критичной логике
