package server

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/auth"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// setSameSiteModeFromConfig устанавливает SameSite атрибут на основе конфигурации.
// Дублирует логику из handlers (чтобы middleware мог корректно удалять cookie).
func setSameSiteModeFromConfig(c *gin.Context, cfg *config.Config) {
	switch cfg.Auth.CookieSameSite {
	case "strict":
		c.SetSameSite(http.SameSiteStrictMode)
	case "lax":
		c.SetSameSite(http.SameSiteLaxMode)
	case "none":
		c.SetSameSite(http.SameSiteNoneMode)
	default:
		c.SetSameSite(http.SameSiteLaxMode)
	}
}

// clearAccessCookie удаляет только access cookie.
// Refresh cookie НЕ трогаем, чтобы клиент мог вызвать /auth/refresh и восстановить access token.
func clearAccessCookie(c *gin.Context, cfg *config.Config) {
	setSameSiteModeFromConfig(c, cfg)
	c.SetCookie(
		cfg.Auth.CookieAccessName,
		"",
		-1,
		"/",
		cfg.Auth.CookieDomain,
		cfg.Auth.CookieSecure,
		cfg.Auth.CookieHttpOnly,
	)
}

// AuthMiddleware проверяет наличие и валидность JWT access токена из httpOnly cookie
// При успешной валидации помещает user_id и role в gin.Context
func AuthMiddleware(cfg *config.Config, store db.Store, logger logging.Logger) gin.HandlerFunc {
	// Создаем auth service для валидации токенов
	authService := auth.NewService(store, cfg, logger)

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
			// Если access token битый/просрочен — очищаем только access cookie,
			// чтобы избежать "вечного 401" на фронте и дать возможность refresh-флоу.
			clearAccessCookie(c, cfg)

			// Различаем истекшие и невалидные токены для фронтенда:
			// - "access_token_expired" — можно обновить через /auth/refresh
			// - "access_token_invalid" — необходим полный re-login
			authError := "access_token_invalid"
			if errors.Is(err, auth.ErrTokenExpired) {
				authError = "access_token_expired"
			}
			c.Header("X-Auth-Error", authError)
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": authError,
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
