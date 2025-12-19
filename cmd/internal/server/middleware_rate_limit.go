package server

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// RateLimiter управляет rate limiting для клиентов
type RateLimiter struct {
	limiter *rate.Limiter
	mu      sync.Mutex
}

// ServiceRateLimitMiddleware создает middleware для rate limiting внутренних сервисов.
// Ограничивает количество запросов для защиты от злоупотреблений в случае компрометации API ключа.
// requests - максимальное количество запросов в секунду
// burst - максимальный размер всплеска запросов
func ServiceRateLimitMiddleware(requests int, burst int) gin.HandlerFunc {
	limiter := &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(requests), burst),
	}

	return func(c *gin.Context) {
		limiter.mu.Lock()
		allowed := limiter.limiter.Allow()
		limiter.mu.Unlock()

		if !allowed {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
