package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// ServiceRateLimitMiddleware создает middleware для rate limiting внутренних сервисов.
// Ограничивает количество запросов для защиты от злоупотреблений в случае компрометации API ключа.
// requests - максимальное количество запросов в секунду
// burst - максимальный размер всплеска запросов
// Примечание: rate.Limiter потокобезопасен, дополнительная синхронизация не требуется.
func ServiceRateLimitMiddleware(requests int, burst int) gin.HandlerFunc {
	limiter := rate.NewLimiter(rate.Limit(requests), burst)

	return func(c *gin.Context) {
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
