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

// NewValidationError formats its arguments using format and returns a *ValidationError whose Message field is set to the formatted string.
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

// NewNotFoundError creates a NotFoundError whose Message is the result of formatting the given format string with the provided args.
func NewNotFoundError(format string, args ...interface{}) error {
	return &NotFoundError{
		Message: fmt.Sprintf(format, args...),
	}
}

// ConflictError представляет конфликт ресурсов (HTTP 409 Conflict).
// Содержит структурированные данные о конфликтах для отображения на фронте.
type ConflictError struct {
	Message   string
	Conflicts interface{} // структурированные данные конфликтов
}

func (e *ConflictError) Error() string {
	return e.Message
}

// NewConflictError creates a ConflictError with the given message and structured conflict data.
func NewConflictError(message string, conflicts interface{}) error {
	return &ConflictError{
		Message:   message,
		Conflicts: conflicts,
	}
}
