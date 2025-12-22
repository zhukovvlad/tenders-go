package util

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ========== Тесты для password_utils.go ==========

func TestHashPassword(t *testing.T) {
	t.Run("успешное хеширование пароля", func(t *testing.T) {
		password := "mysecretpassword123"

		hash, err := HashPassword(password)

		require.NoError(t, err, "хеширование не должно возвращать ошибку")
		assert.NotEmpty(t, hash, "хеш не должен быть пустым")
		assert.NotEqual(t, password, hash, "хеш не должен совпадать с исходным паролем")
		assert.True(t, strings.HasPrefix(hash, "$2a$"), "хеш должен начинаться с префикса bcrypt")
	})

	t.Run("разные пароли дают разные хеши", func(t *testing.T) {
		password1 := "password1"
		password2 := "password2"

		hash1, err1 := HashPassword(password1)
		hash2, err2 := HashPassword(password2)

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.NotEqual(t, hash1, hash2, "разные пароли должны давать разные хеши")
	})

	t.Run("один и тот же пароль дает разные соли", func(t *testing.T) {
		password := "samepassword"

		hash1, err1 := HashPassword(password)
		hash2, err2 := HashPassword(password)

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.NotEqual(t, hash1, hash2, "bcrypt должен генерировать разные соли для одного пароля")
	})
}

func TestHashPassword_EmptyPassword(t *testing.T) {
	t.Run("пустой пароль", func(t *testing.T) {
		password := ""

		hash, err := HashPassword(password)

		// bcrypt может обработать пустую строку, но это не рекомендуется
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

func TestCheckPasswordHash(t *testing.T) {
	t.Run("корректный пароль проходит проверку", func(t *testing.T) {
		password := "correctpassword"
		hash, err := HashPassword(password)
		require.NoError(t, err)

		result := CheckPasswordHash(password, hash)

		assert.True(t, result, "правильный пароль должен пройти проверку")
	})

	t.Run("неверный пароль не проходит проверку", func(t *testing.T) {
		password := "correctpassword"
		wrongPassword := "wrongpassword"
		hash, err := HashPassword(password)
		require.NoError(t, err)

		result := CheckPasswordHash(wrongPassword, hash)

		assert.False(t, result, "неверный пароль не должен пройти проверку")
	})

	t.Run("пустой пароль не проходит проверку с реальным хешем", func(t *testing.T) {
		password := "realpassword"
		hash, err := HashPassword(password)
		require.NoError(t, err)

		result := CheckPasswordHash("", hash)

		assert.False(t, result, "пустой пароль не должен пройти проверку")
	})

	t.Run("проверка с невалидным хешем", func(t *testing.T) {
		password := "anypassword"
		invalidHash := "not-a-valid-bcrypt-hash"

		result := CheckPasswordHash(password, invalidHash)

		assert.False(t, result, "невалидный хеш должен вернуть false")
	})

	t.Run("проверка с пустым хешем", func(t *testing.T) {
		password := "anypassword"

		result := CheckPasswordHash(password, "")

		assert.False(t, result, "пустой хеш должен вернуть false")
	})
}

func TestCheckPasswordHash_WrongPassword(t *testing.T) {
	password := "originalpassword"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	testCases := []struct {
		name          string
		wrongPassword string
	}{
		{"полностью другой пароль", "completelydifferent"},
		{"частично совпадающий пароль", "originalpasswor"},
		{"пароль с дополнительными символами", "originalpassword!"},
		{"пароль в другом регистре", "ORIGINALPASSWORD"},
		{"пароль с пробелами", "original password"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := CheckPasswordHash(tc.wrongPassword, hash)
			assert.False(t, result, "неверный пароль не должен пройти проверку")
		})
	}
}

// ========== Тесты для hash_utils.go ==========

func TestGetSHA256Hash(t *testing.T) {
	t.Run("успешное хеширование строки", func(t *testing.T) {
		text := "test string for hashing"

		hash := GetSHA256Hash(text)

		assert.NotEmpty(t, hash, "хеш не должен быть пустым")
		assert.Equal(t, 64, len(hash), "SHA-256 хеш должен быть 64 символа в hex формате")
	})

	t.Run("одинаковые строки дают одинаковые хеши", func(t *testing.T) {
		text := "consistent text"

		hash1 := GetSHA256Hash(text)
		hash2 := GetSHA256Hash(text)

		assert.Equal(t, hash1, hash2, "одинаковые строки должны давать одинаковые хеши")
	})

	t.Run("разные строки дают разные хеши", func(t *testing.T) {
		text1 := "text one"
		text2 := "text two"

		hash1 := GetSHA256Hash(text1)
		hash2 := GetSHA256Hash(text2)

		assert.NotEqual(t, hash1, hash2, "разные строки должны давать разные хеши")
	})

	t.Run("пустая строка", func(t *testing.T) {
		text := ""

		hash := GetSHA256Hash(text)

		assert.NotEmpty(t, hash, "хеш пустой строки не должен быть пустым")
		assert.Equal(t, 64, len(hash), "хеш должен быть корректной длины")
		// Известный хеш пустой строки в SHA-256
		expectedEmptyHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		assert.Equal(t, expectedEmptyHash, hash, "хеш пустой строки должен соответствовать известному значению")
	})

	t.Run("длинная строка", func(t *testing.T) {
		text := strings.Repeat("a", 10000)

		hash := GetSHA256Hash(text)

		assert.NotEmpty(t, hash)
		assert.Equal(t, 64, len(hash), "хеш длинной строки должен быть той же длины")
	})

	t.Run("строка с юникодом", func(t *testing.T) {
		text := "Привет мир! 你好世界 🌍"

		hash := GetSHA256Hash(text)

		assert.NotEmpty(t, hash)
		assert.Equal(t, 64, len(hash))
	})

	t.Run("строка с специальными символами", func(t *testing.T) {
		text := "!@#$%^&*()_+-=[]{}|;:',.<>?/~`"

		hash := GetSHA256Hash(text)

		assert.NotEmpty(t, hash)
		assert.Equal(t, 64, len(hash))
	})

	t.Run("детерминированность хеша", func(t *testing.T) {
		text := "deterministic test"
		iterations := 100

		firstHash := GetSHA256Hash(text)

		for i := 0; i < iterations; i++ {
			hash := GetSHA256Hash(text)
			assert.Equal(t, firstHash, hash, "хеш должен быть детерминированным")
		}
	})
}

// ========== Бенчмарки ==========

func BenchmarkHashPassword(b *testing.B) {
	password := "benchmarkpassword123"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword(password)
	}
}

func BenchmarkCheckPasswordHash(b *testing.B) {
	password := "benchmarkpassword123"
	hash, _ := HashPassword(password)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckPasswordHash(password, hash)
	}
}

func BenchmarkGetSHA256Hash(b *testing.B) {
	text := "benchmark text for sha256 hashing"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetSHA256Hash(text)
	}
}

func BenchmarkGetSHA256Hash_LongString(b *testing.B) {
	text := strings.Repeat("a", 10000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GetSHA256Hash(text)
	}
}
