.PHONY: all postgres createdb dropdb migrateup migratedown dockerstart dockerstop stop-and-remove-db sqlc run setup-db generate-env createadmin test test-unit test-integration test-e2e test-coverage test-watch

# --- Переменные ---
CONTAINER_NAME = postgres-tender
DB_USER = root
DB_PASSWORD = secret
DB_NAME = tendersdb
DB_PORT = 5435
DOCKER_IMAGE = pgvector/pgvector:pg17
DB_DATA_PATH = $(shell pwd)/postgres_data
DB_URL = "postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable"
MIGRATION_PATH = "cmd/internal/db/migration"
GOPATH = $(shell go env GOPATH)

# --- Основные команды ---

all: run # Команда по умолчанию

run:
	@if [ -f .env ]; then set -a && . ./.env && set +a; fi && go run cmd/main/app.go

sqlc:
	$(GOPATH)/bin/sqlc generate

# Генерирует безопасный API ключ в формате GO_SERVER_API_KEY=<key>
# Скопируйте вывод в ваш .env файл
generate-env:
	@./scripts/generate-env.sh

# --- Команды для БД ---

setup-db: postgres
	@echo "Ожидание запуска PostgreSQL (5 секунд)..."
	@sleep 5
	@make createdb
	@make migrateup

postgres:
	@echo "Запуск контейнера PostgreSQL..."
	@mkdir -p $(dir $(DB_DATA_PATH))
	docker run --name $(CONTAINER_NAME) -p $(DB_PORT):5432 -e POSTGRES_USER=$(DB_USER) -e POSTGRES_PASSWORD=$(DB_PASSWORD) -d -v $(DB_DATA_PATH):/var/lib/postgresql/data $(DOCKER_IMAGE)
	@echo "Контейнер запущен. Данные хранятся в $(DB_DATA_PATH)"

docker-start:
	docker start $(CONTAINER_NAME)

docker-stop:
	docker stop $(CONTAINER_NAME)

stop-and-remove-db: docker-stop
	@echo "Удаление контейнера..."
	@docker rm $(CONTAINER_NAME) || true

createdb:
	docker exec -it $(CONTAINER_NAME) createdb --username=$(DB_USER) --owner=$(DB_USER) $(DB_NAME)

dropdb:
	docker exec -it $(CONTAINER_NAME) dropdb $(DB_NAME)

migrateup:
	$(GOPATH)/bin/migrate -path $(MIGRATION_PATH) -database "$(DB_URL)" -verbose up

migratedown:
	$(GOPATH)/bin/migrate -path $(MIGRATION_PATH) -database "$(DB_URL)" -verbose down
# --- CLI команды ---

createadmin:
	@echo "Creating admin user..."
	@go run cmd/createadmin/main.go

# --- Тестирование ---

test: test-unit ## Запуск всех тестов (unit + integration)
	@echo "Running all tests..."

test-unit: ## Запуск только unit-тестов
	@echo "Running unit tests..."
	@go test -v -race -short ./cmd/internal/...

test-integration: ## Запуск интеграционных тестов (требуется Docker)
	@echo "Running integration tests..."
	@go test -v -race -timeout 5m ./tests/integration/...

test-e2e: ## Запуск E2E тестов (требуется Docker)
	@echo "Running E2E tests..."
	@go test -v -race -timeout 10m ./tests/e2e/...

test-coverage: ## Запуск тестов с отчетом о покрытии
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out -covermode=atomic ./cmd/internal/... ./tests/...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@go tool cover -func=coverage.out | grep total

test-watch: ## Watch mode для тестов (требуется entr: apt install entr)
	@echo "Starting test watch mode (Ctrl+C to stop)..."
	@find . -name "*.go" | entr -c make test-unit
