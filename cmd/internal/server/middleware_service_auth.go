package server

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// ServiceBearerAuthMiddleware создает middleware для аутентификации внутренних сервисов
// используя Bearer токен из заголовка Authorization.
// serviceName используется для идентификации сервиса в контексте запроса.
func ServiceBearerAuthMiddleware(serviceName string) gin.HandlerFunc {
	secret := os.Getenv("GO_SERVER_API_KEY")
	if secret == "" {
		panic("GO_SERVER_API_KEY environment variable is not set - run 'make generate-env' to create one")
	}

	secretBytes := []byte(secret)

	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "service auth required"})
			return
		}

		token := []byte(h[7:])
		if subtle.ConstantTimeCompare(token, secretBytes) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid service token"})
			return
		}

		c.Set("service", serviceName)
		c.Next()
	}
}
