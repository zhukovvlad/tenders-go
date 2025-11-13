package apierrors

import "fmt"

// ValidationError представляет ошибку валидации входных данных.
// Используется для разделения ошибок валидации (HTTP 400) от серверных ошибок (HTTP 500).
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

// NewValidationError создает новую ошибку валидации.
func NewValidationError(format string, args ...interface{}) error {
	return &ValidationError{
		Message: fmt.Sprintf(format, args...),
	}
}

// NotFoundError представляет ошибку "ресурс не найден".
// Используется для возврата HTTP 404 Not Found.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// NewNotFoundError создает новую ошибку "не найдено".
func NewNotFoundError(format string, args ...interface{}) error {
	return &NotFoundError{
		Message: fmt.Sprintf(format, args...),
	}
}