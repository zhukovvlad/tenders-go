package util

import (
	"crypto/sha256"
	"encoding/hex"
)

// GetSHA256Hash вычисляет хеш SHA-256 для входной строки.
// Используется для создания стабильных, анонимных ключей
// для `matching_cache`.
func GetSHA256Hash(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}