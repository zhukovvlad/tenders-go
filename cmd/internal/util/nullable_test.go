package util

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ========== Тесты для Deref ==========

func TestDeref(t *testing.T) {
	t.Run("разыменование непустого указателя", func(t *testing.T) {
		str := "test string"
		result := Deref(&str)
		assert.Equal(t, "test string", result)
	})

	t.Run("разыменование nil указателя", func(t *testing.T) {
		result := Deref(nil)
		assert.Equal(t, "", result)
	})

	t.Run("разыменование пустой строки", func(t *testing.T) {
		str := ""
		result := Deref(&str)
		assert.Equal(t, "", result)
	})
}

// ========== Тесты для NullableString ==========

func TestNullableString(t *testing.T) {
	t.Run("валидная строка", func(t *testing.T) {
		str := "valid string"
		result := NullableString(&str)

		assert.True(t, result.Valid)
		assert.Equal(t, "valid string", result.String)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableString(nil)

		assert.False(t, result.Valid)
	})

	t.Run("пустая строка", func(t *testing.T) {
		str := ""
		result := NullableString(&str)

		assert.False(t, result.Valid, "пустая строка должна быть невалидной")
	})

	t.Run("строка с пробелами", func(t *testing.T) {
		str := "   "
		result := NullableString(&str)

		assert.True(t, result.Valid, "строка с пробелами валидна")
		assert.Equal(t, "   ", result.String)
	})
}

// ========== Тесты для NullableFloat64 ==========

func TestNullableFloat64(t *testing.T) {
	t.Run("валидное значение", func(t *testing.T) {
		val := 123.45
		result := NullableFloat64(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, 123.45, result.Float64)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableFloat64(nil)

		assert.False(t, result.Valid)
	})

	t.Run("нулевое значение", func(t *testing.T) {
		val := 0.0
		result := NullableFloat64(&val)

		assert.True(t, result.Valid, "0.0 должен быть валидным")
		assert.Equal(t, 0.0, result.Float64)
	})

	t.Run("отрицательное значение", func(t *testing.T) {
		val := -99.99
		result := NullableFloat64(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, -99.99, result.Float64)
	})
}

// ========== Тесты для NullableInt32 ==========

func TestNullableInt32(t *testing.T) {
	t.Run("валидное значение", func(t *testing.T) {
		val := 42
		result := NullableInt32(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, int32(42), result.Int32)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableInt32(nil)

		assert.False(t, result.Valid)
	})

	t.Run("нулевое значение", func(t *testing.T) {
		val := 0
		result := NullableInt32(&val)

		assert.True(t, result.Valid, "0 должен быть валидным")
		assert.Equal(t, int32(0), result.Int32)
	})

	t.Run("отрицательное значение", func(t *testing.T) {
		val := -100
		result := NullableInt32(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, int32(-100), result.Int32)
	})
}

// ========== Тесты для NullableInt64 ==========

func TestNullableInt64(t *testing.T) {
	t.Run("валидное значение", func(t *testing.T) {
		val := int64(1234567890)
		result := NullableInt64(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, int64(1234567890), result.Int64)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableInt64(nil)

		assert.False(t, result.Valid)
	})

	t.Run("нулевое значение", func(t *testing.T) {
		val := int64(0)
		result := NullableInt64(&val)

		assert.True(t, result.Valid)
		assert.Equal(t, int64(0), result.Int64)
	})
}

// ========== Тесты для NullableBool ==========

func TestNullableBool(t *testing.T) {
	t.Run("true значение", func(t *testing.T) {
		val := true
		result := NullableBool(&val)

		assert.True(t, result.Valid)
		assert.True(t, result.Bool)
	})

	t.Run("false значение", func(t *testing.T) {
		val := false
		result := NullableBool(&val)

		assert.True(t, result.Valid, "false должен быть валидным")
		assert.False(t, result.Bool)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableBool(nil)

		assert.False(t, result.Valid)
	})
}

// ========== Тесты для NullableTime ==========

func TestNullableTime(t *testing.T) {
	t.Run("валидное время", func(t *testing.T) {
		now := time.Now()
		result := NullableTime(&now)

		assert.True(t, result.Valid)
		assert.Equal(t, now, result.Time)
	})

	t.Run("nil указатель", func(t *testing.T) {
		result := NullableTime(nil)

		assert.False(t, result.Valid)
	})

	t.Run("zero time", func(t *testing.T) {
		zeroTime := time.Time{}
		result := NullableTime(&zeroTime)

		assert.True(t, result.Valid, "zero time должен быть валидным")
		assert.True(t, result.Time.IsZero())
	})
}

// ========== Тесты для NilIfEmpty ==========

func TestNilIfEmpty(t *testing.T) {
	t.Run("пустая строка возвращает nil", func(t *testing.T) {
		result := NilIfEmpty("")

		assert.Nil(t, result)
	})

	t.Run("непустая строка возвращает указатель", func(t *testing.T) {
		result := NilIfEmpty("test")

		assert.NotNil(t, result)
		assert.Equal(t, "test", *result)
	})

	t.Run("строка с пробелами возвращает указатель", func(t *testing.T) {
		result := NilIfEmpty("  ")

		assert.NotNil(t, result)
		assert.Equal(t, "  ", *result)
	})
}

// ========== Тесты для IntPointerOrNil ==========

func TestIntPointerOrNil(t *testing.T) {
	t.Run("нулевое значение возвращает nil", func(t *testing.T) {
		result := IntPointerOrNil(0)

		assert.Nil(t, result)
	})

	t.Run("ненулевое значение возвращает указатель", func(t *testing.T) {
		result := IntPointerOrNil(42)

		assert.NotNil(t, result)
		assert.Equal(t, 42, *result)
	})

	t.Run("отрицательное значение возвращает указатель", func(t *testing.T) {
		result := IntPointerOrNil(-5)

		assert.NotNil(t, result)
		assert.Equal(t, -5, *result)
	})
}

// ========== Тесты для ConvertNullFloat64ToNullString ==========

func TestConvertNullFloat64ToNullString(t *testing.T) {
	t.Run("валидное float64 значение", func(t *testing.T) {
		input := sql.NullFloat64{Float64: 123.45, Valid: true}
		result := ConvertNullFloat64ToNullString(input)

		assert.True(t, result.Valid)
		assert.Equal(t, "123.45", result.String)
	})

	t.Run("невалидное значение", func(t *testing.T) {
		input := sql.NullFloat64{Valid: false}
		result := ConvertNullFloat64ToNullString(input)

		assert.False(t, result.Valid)
	})

	t.Run("нулевое значение", func(t *testing.T) {
		input := sql.NullFloat64{Float64: 0.0, Valid: true}
		result := ConvertNullFloat64ToNullString(input)

		assert.True(t, result.Valid)
		assert.Equal(t, "0", result.String)
	})

	t.Run("отрицательное значение", func(t *testing.T) {
		input := sql.NullFloat64{Float64: -99.99, Valid: true}
		result := ConvertNullFloat64ToNullString(input)

		assert.True(t, result.Valid)
		assert.Equal(t, "-99.99", result.String)
	})

	t.Run("очень большое число", func(t *testing.T) {
		input := sql.NullFloat64{Float64: 1234567890.123456, Valid: true}
		result := ConvertNullFloat64ToNullString(input)

		assert.True(t, result.Valid)
		assert.Contains(t, result.String, "1234567890")
	})
}

// ========== Тесты для ParseDate ==========

func TestParseDate(t *testing.T) {
	t.Run("валидная дата", func(t *testing.T) {
		dateStr := "21.12.2025 15:30:45"
		result := ParseDate(dateStr)

		assert.True(t, result.Valid)
		assert.Equal(t, 2025, result.Time.Year())
		assert.Equal(t, time.December, result.Time.Month())
		assert.Equal(t, 21, result.Time.Day())
		assert.Equal(t, 15, result.Time.Hour())
		assert.Equal(t, 30, result.Time.Minute())
		assert.Equal(t, 45, result.Time.Second())
	})

	t.Run("пустая строка", func(t *testing.T) {
		result := ParseDate("")

		assert.False(t, result.Valid)
	})

	t.Run("невалидный формат", func(t *testing.T) {
		dateStr := "2025-12-21 15:30:45" // Неправильный формат
		result := ParseDate(dateStr)

		assert.False(t, result.Valid)
	})

	t.Run("частично валидный формат", func(t *testing.T) {
		dateStr := "21.12.2025" // Без времени
		result := ParseDate(dateStr)

		assert.False(t, result.Valid)
	})

	t.Run("невалидные значения", func(t *testing.T) {
		dateStr := "32.13.2025 25:99:99" // Несуществующая дата
		result := ParseDate(dateStr)

		assert.False(t, result.Valid)
	})

	t.Run("граничное значение - начало года", func(t *testing.T) {
		dateStr := "01.01.2025 00:00:00"
		result := ParseDate(dateStr)

		assert.True(t, result.Valid)
		assert.Equal(t, 2025, result.Time.Year())
		assert.Equal(t, time.January, result.Time.Month())
		assert.Equal(t, 1, result.Time.Day())
	})

	t.Run("граничное значение - конец года", func(t *testing.T) {
		dateStr := "31.12.2025 23:59:59"
		result := ParseDate(dateStr)

		assert.True(t, result.Valid)
		assert.Equal(t, 2025, result.Time.Year())
		assert.Equal(t, time.December, result.Time.Month())
		assert.Equal(t, 31, result.Time.Day())
		assert.Equal(t, 23, result.Time.Hour())
		assert.Equal(t, 59, result.Time.Minute())
		assert.Equal(t, 59, result.Time.Second())
	})
}
