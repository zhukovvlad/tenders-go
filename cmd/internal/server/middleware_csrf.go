package server

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
)

// CsrfMiddleware проверяет CSRF только для "опасных" методов.
// Логика: cookie(csrf_token) должна совпадать с header(X-CSRF-Token).
func CsrfMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		// Пропускаем CSRF для эндпоинтов без токена (login - первый вход, refresh - восстановление)
		// Используем FullPath() для точного сопоставления с зарегистрированным роутом Gin
		path := c.FullPath()
		if path == "" {
			// Fallback для случаев когда роут не найден
			path = c.Request.URL.Path
		}
		if path == "/api/v1/auth/login" || path == "/api/v1/auth/refresh" {
			c.Next()
			return
		}

		csrfCookie, err := c.Cookie(csrfCookieName)
		if err != nil || csrfCookie == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "csrf_token_missing"})
			c.Abort()
			return
		}

		csrfHeader := c.GetHeader(csrfHeaderName)
		if csrfHeader == "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "csrf_header_missing"})
			c.Abort()
			return
		}

		if subtle.ConstantTimeCompare([]byte(csrfCookie), []byte(csrfHeader)) != 1 {
			c.JSON(http.StatusForbidden, gin.H{"error": "csrf_invalid"})
			c.Abort()
			return
		}

		c.Next()
	}
}
