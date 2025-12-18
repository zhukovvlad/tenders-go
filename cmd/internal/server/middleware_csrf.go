package server

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
)

// CsrfMiddleware проверяет CSRF только для "опасных" методов.
// Логика: cookie(csrf_token) должна совпадать с header(X-CSRF-Token).
func CsrfMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			c.Next()
			return
		}

		// Разрешаем login без CSRF (иначе будет сложно стартовать сессию)
		// Используем FullPath() для точного сопоставления с роутом
		p := c.FullPath()
		if p == "" {
			p = c.Request.URL.Path
		}
		if p == "/api/v1/auth/login" || strings.TrimRight(p, "/") == "/api/v1/auth/login" {
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
