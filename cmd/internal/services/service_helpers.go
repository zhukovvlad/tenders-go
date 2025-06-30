package services

import (
	"context"
	"database/sql"
	"fmt"

	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

func getOrCreateOrUpdate[T any, P any](
	_ context.Context,
	_ db.Querier,
	// Функция для получения существующей сущности
	getFn func() (T, error),
	// Функция для создания новой сущности
	createFn func() (T, error),
	// Функция, которая проверяет, нужно ли обновление.
	// Возвращает:
	// 1. bool - нужно ли обновление.
	// 2. P - параметры для обновления.
	// 3. error - если ошибка.
	diffFn func(existing T) (bool, P, error),
	// Функция для выполнения обновления
	updateFn func(params P) (T, error),
) (T, error) {
	existing, err := getFn()
	if err != nil {
		if err == sql.ErrNoRows {
			// Сущность не найдена, создаем новую
			return createFn()
		}

		var zero T
		return zero, err
	}

	// Сущность найдена, проверяем необходимость обновления
	needsUpdate, updateParams, err := diffFn(existing)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("ошибка при проверке необходимости обновления: %w", err)
	}

	if needsUpdate {
		return updateFn(updateParams)
	}

	return existing, nil
}