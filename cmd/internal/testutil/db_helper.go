package testutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresContainer представляет контейнер PostgreSQL для тестирования
type PostgresContainer struct {
	Container testcontainers.Container
	DSN       string
}

// SetupTestDatabase создает и запускает PostgreSQL контейнер для тестов
func SetupTestDatabase(t *testing.T) (*sql.DB, *PostgresContainer, error) {
	t.Helper()
	return setupTestDatabaseCore()
}

// SetupTestDatabaseNoT создает PostgreSQL контейнер без *testing.T (для TestMain).
func SetupTestDatabaseNoT() (*sql.DB, *PostgresContainer, error) {
	return setupTestDatabaseCore()
}

func setupTestDatabaseCore() (*sql.DB, *PostgresContainer, error) {
	ctx := context.Background()

	// Настройка контейнера PostgreSQL с pgvector
	req := testcontainers.ContainerRequest{
		Image:        "pgvector/pgvector:pg17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_USER":     "testuser",
			"POSTGRES_PASSWORD": "testpass",
			"POSTGRES_DB":       "testdb",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
			wait.ForListeningPort("5432/tcp"),
		),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to start container: %w", err)
	}

	// Helper to cleanup container on error
	cleanup := func() {
		_ = container.Terminate(ctx)
	}

	// Получение хоста и порта контейнера
	host, err := container.Host(ctx)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to get container host: %w", err)
	}

	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to get container port: %w", err)
	}

	// Формирование DSN
	dsn := fmt.Sprintf("postgres://testuser:testpass@%s:%s/testdb?sslmode=disable", host, port.Port())

	// Подключение к БД
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Проверка подключения
	if err := db.Ping(); err != nil {
		db.Close()
		cleanup()
		return nil, nil, fmt.Errorf("failed to ping database: %w", err)
	}

	pgContainer := &PostgresContainer{
		Container: container,
		DSN:       dsn,
	}

	return db, pgContainer, nil
}

// TeardownTestDatabase останавливает и удаляет контейнер PostgreSQL
func TeardownTestDatabase(t *testing.T, db *sql.DB, container *PostgresContainer) {
	t.Helper()
	if err := TeardownTestDatabaseNoT(db, container); err != nil {
		t.Errorf("teardown error: %v", err)
	}
}

// TeardownTestDatabaseNoT останавливает контейнер без *testing.T (для TestMain).
func TeardownTestDatabaseNoT(db *sql.DB, container *PostgresContainer) error {
	var errs []error
	if db != nil {
		if err := db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close database: %w", err))
		}
	}

	if container != nil && container.Container != nil {
		ctx := context.Background()
		if err := container.Container.Terminate(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to terminate container: %w", err))
		}
	}
	return errors.Join(errs...)
}

// LogFunc is a function signature for logging (compatible with both t.Logf and log.Printf).
type LogFunc func(format string, args ...any)

// RunMigrations применяет SQL миграции к тестовой БД с логированием в тестовый вывод.
func RunMigrations(t *testing.T, db *sql.DB) error {
	t.Helper()
	return runMigrationsCore(db, t.Logf)
}

// RunMigrationsNoT применяет миграции без *testing.T (для TestMain).
func RunMigrationsNoT(db *sql.DB) error {
	return runMigrationsCore(db, log.Printf)
}

// runMigrationsCore — общая реализация применения миграций с настраиваемым логгером.
func runMigrationsCore(db *sql.DB, logf LogFunc) error {
	// Получаем путь к директории с миграциями
	// db_helper.go is at cmd/internal/testutil/db_helper.go
	// migrations are at cmd/internal/db/migration/
	// So from db_helper.go's dir: ../db/migration
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("runtime.Caller(0) failed: unable to determine db_helper.go location")
	}
	migrationsPath := filepath.Join(filepath.Dir(filename), "..", "db", "migration")

	// Читаем файлы миграций
	files, err := filepath.Glob(filepath.Join(migrationsPath, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("failed to read migration files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no migration files found in %s", migrationsPath)
	}

	// Сортируем миграции для правильного порядка выполнения
	sort.Strings(files)

	// Применяем миграции
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		_, err = db.Exec(string(content))
		if err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
		logf("Applied migration: %s", file)
	}

	return nil
}

// CleanupTables очищает все таблицы в БД между тестами
func CleanupTables(t *testing.T, db *sql.DB) error {
	t.Helper()

	tables := []string{
		"proposals",
		"catalog_items",
		"lots",
		"tenders",
		"chapters",
		"tender_categories",
		"tender_types",
		"users",
	}

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for _, table := range tables {
		query := fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)
		if _, err := tx.ExecContext(ctx, query); err != nil {
			// PostgreSQL error code 42P01 = undefined_table
			if strings.Contains(err.Error(), "42P01") || strings.Contains(err.Error(), "does not exist") {
				t.Logf("Skipping non-existent table: %s", table)
				continue
			}
			return fmt.Errorf("failed to truncate table %s: %w", table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// WaitForDatabase ожидает, пока БД не станет доступной
func WaitForDatabase(db *sql.DB, maxRetries int) error {
	for i := 0; i < maxRetries; i++ {
		if err := db.Ping(); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("database not available after %d retries", maxRetries)
}
