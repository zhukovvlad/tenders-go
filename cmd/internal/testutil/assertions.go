package testutil

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertJSONEqual сравнивает два JSON объекта независимо от порядка полей
func AssertJSONEqual(t *testing.T, expected, actual string) {
	t.Helper()

	var expectedJSON, actualJSON interface{}

	err := json.Unmarshal([]byte(expected), &expectedJSON)
	require.NoError(t, err, "Invalid expected JSON")

	err = json.Unmarshal([]byte(actual), &actualJSON)
	require.NoError(t, err, "Invalid actual JSON")

	assert.Equal(t, expectedJSON, actualJSON)
}

// AssertErrorContains проверяет, что ошибка содержит определенную подстроку
func AssertErrorContains(t *testing.T, err error, substring string) {
	t.Helper()

	require.Error(t, err, "Expected an error but got nil")
	assert.Contains(t, err.Error(), substring)
}

// AssertNoError проверяет отсутствие ошибки с подробным сообщением
func AssertNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	require.NoError(t, err, msgAndArgs...)
}

// AssertEqual проверяет равенство с подробным сообщением
func AssertEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Equal(t, expected, actual, msgAndArgs...)
}

// AssertNotEmpty проверяет, что значение не пустое
func AssertNotEmpty(t *testing.T, obj interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.NotEmpty(t, obj, msgAndArgs...)
}

// AssertNil проверяет, что значение nil
func AssertNil(t *testing.T, obj interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Nil(t, obj, msgAndArgs...)
}

// AssertNotNil проверяет, что значение не nil
func AssertNotNil(t *testing.T, obj interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.NotNil(t, obj, msgAndArgs...)
}

// AssertGreaterThan проверяет, что значение больше заданного
func AssertGreaterThan(t *testing.T, e1, e2 interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Greater(t, e1, e2, msgAndArgs...)
}

// AssertLessThan проверяет, что значение меньше заданного
func AssertLessThan(t *testing.T, e1, e2 interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Less(t, e1, e2, msgAndArgs...)
}

// AssertTrue проверяет, что значение true
func AssertTrue(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	assert.True(t, value, msgAndArgs...)
}

// AssertFalse проверяет, что значение false
func AssertFalse(t *testing.T, value bool, msgAndArgs ...interface{}) {
	t.Helper()
	assert.False(t, value, msgAndArgs...)
}

// AssertContains проверяет, что строка содержит подстроку
func AssertContains(t *testing.T, s, substr string, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Contains(t, s, substr, msgAndArgs...)
}

// AssertLen проверяет длину коллекции
func AssertLen(t *testing.T, object interface{}, length int, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Len(t, object, length, msgAndArgs...)
}

// AssertWithinDuration проверяет, что два времени находятся в пределах заданной длительности
func AssertWithinDuration(t *testing.T, expected, actual time.Time, delta time.Duration, msgAndArgs ...interface{}) {
	t.Helper()
	assert.WithinDuration(t, expected, actual, delta, msgAndArgs...)
}
