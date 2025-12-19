package server

import (
	"crypto/subtle"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

func ServiceBearerAuthMiddleware() gin.HandlerFunc {
	secret := os.Getenv("GO_SERVER_API_KEY")
	if secret == "" {
		panic("GO_SERVER_API_KEY not set")
	}

	secretBytes := []byte(secret)

	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if len(h) < 8 || h[:7] != "Bearer " {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "service auth required"})
			return
		}

		token := []byte(h[7:])
		if subtle.ConstantTimeCompare(token, secretBytes) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid service token"})
			return
		}

		c.Set("service", "python-worker")
		c.Next()
	}
}
