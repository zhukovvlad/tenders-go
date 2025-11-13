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