.PHONY: all postgres createdb dropdb migrateup migratedown dockerstart dockerstop stop-and-remove-db sqlc run setup-db

# --- Переменные ---
CONTAINER_NAME = postgres-tender
DB_USER = root
DB_PASSWORD = secret
DB_NAME = tendersdb
DB_PORT = 5435
DOCKER_IMAGE = pgvector/pgvector:pg17
DB_DATA_PATH = "$(pwd)/postgres_data"
DB_URL = "postgres://$(DB_USER):$(DB_PASSWORD)@localhost:$(DB_PORT)/$(DB_NAME)?sslmode=disable"
MIGRATION_PATH = "cmd/internal/db/migration"

# --- Основные команды ---

all: run # Команда по умолчанию

run:
	go run cmd/main/app.go

sqlc:
	sqlc generate

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
	migrate -path $(MIGRATION_PATH) -database "$(DB_URL)" -verbose up

migratedown:
	migrate -path $(MIGRATION_PATH) -database "$(DB_URL)" -verbose down