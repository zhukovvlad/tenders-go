package server

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/auth"
)

// AuthMiddleware проверяет наличие и валидность JWT access токена из httpOnly cookie
// При успешной валидации помещает user_id и role в gin.Context
func AuthMiddleware(cfg *config.Config, store db.Store) gin.HandlerFunc {
	// Создаем auth service для валидации токенов
	authService := auth.NewService(store, cfg)

	return func(c *gin.Context) {
		// Извлекаем access token из cookie
		accessToken, err := c.Cookie(cfg.Auth.CookieAccessName)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "access token not found",
			})
			c.Abort()
			return
		}

		// Валидируем токен
		claims, err := authService.ValidateAccessToken(accessToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired access token",
			})
			c.Abort()
			return
		}

		// Сохраняем user_id и role в context
		c.Set("user_id", claims.UserID)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// RequireRole проверяет, что у пользователя есть требуемая роль
// Должна использоваться после AuthMiddleware
func RequireRole(requiredRole string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Извлекаем role из context
		roleValue, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authentication required",
			})
			c.Abort()
			return
		}

		role, ok := roleValue.(string)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "invalid role type in context",
			})
			c.Abort()
			return
		}

		// Проверяем роль
		if role != requiredRole {
			c.JSON(http.StatusForbidden, gin.H{
				"error": "insufficient permissions",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
