package server

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
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
	logger := logging.GetLogger()

	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			logger.Warnf("Service auth failed: missing or invalid Authorization header from %s", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "service auth required"})
			return
		}

		token := []byte(h[7:])
		if subtle.ConstantTimeCompare(token, secretBytes) != 1 {
			logger.Warnf("Service auth failed: invalid token from %s", c.ClientIP())
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid service token"})
			return
		}

		// Сохраняем имя сервиса в контексте для использования в хендлерах
		c.Set("service", serviceName)

		// Логируем успешную аутентификацию сервиса
		logger.Infof("Service authenticated: %s from %s -> %s %s", serviceName, c.ClientIP(), c.Request.Method, c.Request.URL.Path)

		c.Next()
	}
}
