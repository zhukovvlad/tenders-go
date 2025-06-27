package util

import (
	"database/sql"
	"strconv"
	"time" // Понадобится для NullableTime
)

// NullableString преобразует *string в sql.NullString.
// Пустая строка ("") также будет считаться NULL для базы данных.
func NullableString(s *string) sql.NullString {
	if s == nil || *s == "" { // Если указатель nil ИЛИ строка пустая
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

// NullableFloat64 преобразует *float64 в sql.NullFloat64.
func NullableFloat64(f *float64) sql.NullFloat64 {
	if f == nil {
		return sql.NullFloat64{Valid: false}
	}
	return sql.NullFloat64{Float64: *f, Valid: true}
}

// NullableInt32 преобразует *int в sql.NullInt32.
// Это полезно, если в ваших api_models поля типа ContractorWidth/Height станут *int,
// чтобы явно показать, что они могут отсутствовать (а не просто быть 0).
func NullableInt32(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

// NullableInt64 преобразует *int64 в sql.NullInt64.
// Может пригодиться для nullable внешних ключей или других bigint полей.
func NullableInt64(i *int64) sql.NullInt64 {
	if i == nil {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: *i, Valid: true}
}

// NullableBool преобразует *bool в sql.NullBool.
func NullableBool(b *bool) sql.NullBool {
	if b == nil {
		return sql.NullBool{Valid: false}
	}
	return sql.NullBool{Bool: *b, Valid: true}
}

// NullableTime преобразует *time.Time в sql.NullTime.
func NullableTime(t *time.Time) sql.NullTime {
    if t == nil {
        return sql.NullTime{Valid: false}
    }
    return sql.NullTime{Time: *t, Valid: true}
}

// Для строковых полей, если пустая строка не должна передаваться как валидная (а как NULL)
func NilIfEmpty(s string) *string {
    if s == "" {
        return nil
    }
    return &s
}

// Дополнительный хелпер для int -> *int, если в api_models поля int, а для nullable нужен *int
func IntPointerOrNil(val int) *int {
	if val == 0 { // Осторожно: 0 может быть валидным значением. 
                  // Лучше, если в api_models поля, которые могут отсутствовать, будут *int
		return nil
	}
	return &val
}

// ConvertNullFloat64ToNullString преобразует sql.NullFloat64 в sql.NullString.
func ConvertNullFloat64ToNullString(nf sql.NullFloat64) sql.NullString {
	if !nf.Valid {
		return sql.NullString{Valid: false}
	}
	// Преобразуем float64 в строку.
	// strconv.FormatFloat предлагает хороший контроль над форматированием.
	// 'f' - для стандартного десятичного представления (-ddd.dddd)
	// -1 - для минимально необходимого количества знаков после запятой
	// 64 - для float64
	s := strconv.FormatFloat(nf.Float64, 'f', -1, 64)
	return sql.NullString{String: s, Valid: true}
}