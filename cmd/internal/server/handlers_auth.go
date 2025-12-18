package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zhukovvlad/tenders-go/cmd/internal/services/auth"
)

// LoginRequest содержит данные для аутентификации
type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// loginHandler обрабатывает POST /api/v1/auth/login
// Аутентификация пользователя по email и паролю, возврат access и refresh токенов в httpOnly cookies
func (s *Server) loginHandler(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request format"})
		return
	}

	// Получаем IP адрес клиента и User-Agent
	ipAddress := parseIPAddress(c.ClientIP())
	userAgent := c.Request.UserAgent()

	// Выполняем аутентификацию
	result, err := s.authService.Login(c.Request.Context(), req.Email, req.Password, ipAddress, userAgent)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		s.logger.WithError(err).Error("login failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Устанавливаем cookies
	s.setAuthCookies(c, result.AccessToken, result.RefreshToken)

	// Возвращаем информацию о пользователе
	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":    result.User.ID,
			"email": result.User.Email,
			"role":  result.User.Role,
		},
	})
}

// refreshHandler обрабатывает POST /api/v1/auth/refresh
// Обновление access токена с использованием refresh токена из cookie
func (s *Server) refreshHandler(c *gin.Context) {
	// Извлекаем refresh token из cookie
	refreshToken, err := c.Cookie(s.config.Auth.CookieRefreshName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "refresh token not found"})
		return
	}

	// Получаем IP адрес и User-Agent
	ipAddress := parseIPAddress(c.ClientIP())
	userAgent := c.Request.UserAgent()

	// Обновляем токены
	result, err := s.authService.Refresh(c.Request.Context(), refreshToken, ipAddress, userAgent)
	if err != nil {
		if errors.Is(err, auth.ErrSessionNotFound) || errors.Is(err, auth.ErrInvalidToken) {
			// Очищаем cookies при невалидном refresh token
			s.clearAuthCookies(c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired refresh token"})
			return
		}
		s.logger.WithError(err).Error("refresh failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Устанавливаем новые cookies
	s.setAuthCookies(c, result.AccessToken, result.RefreshToken)

	c.JSON(http.StatusOK, gin.H{
		"message": "tokens refreshed successfully",
	})
}

// logoutHandler обрабатывает POST /api/v1/auth/logout
// Отзыв refresh токена и удаление cookies
func (s *Server) logoutHandler(c *gin.Context) {
	// Извлекаем refresh token из cookie
	refreshToken, err := c.Cookie(s.config.Auth.CookieRefreshName)
	if err != nil {
		// Даже если cookie нет, очищаем их на всякий случай
		s.clearAuthCookies(c)
		c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
		return
	}

	// Отзываем сессию
	if err := s.authService.Logout(c.Request.Context(), refreshToken); err != nil {
		s.logger.WithError(err).Error("logout failed")
		// Не возвращаем ошибку пользователю, все равно очищаем cookies
	}

	// Очищаем cookies
	s.clearAuthCookies(c)

	c.JSON(http.StatusOK, gin.H{"message": "logged out successfully"})
}

// meHandler обрабатывает GET /api/v1/auth/me
// Возврат информации о текущем аутентифицированном пользователе
func (s *Server) meHandler(c *gin.Context) {
	// Извлекаем user_id из context (установлен AuthMiddleware)
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "user not authenticated"})
		return
	}

	// Проверяем тип
	userIDVal, ok := userID.(int64)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid user_id type"})
		return
	}

	// Получаем полную информацию о пользователе
	user, err := s.store.GetUserByID(c.Request.Context(), userIDVal)
	if err != nil {
		s.logger.WithError(err).Error("failed to get user")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
	})
}

// setSameSiteMode устанавливает SameSite атрибут на основе конфигурации
func (s *Server) setSameSiteMode(c *gin.Context) {
	switch s.config.Auth.CookieSameSite {
	case "strict":
		c.SetSameSite(http.SameSiteStrictMode)
	case "lax":
		c.SetSameSite(http.SameSiteLaxMode)
	case "none":
		c.SetSameSite(http.SameSiteNoneMode)
	default:
		// Fallback на Lax режим для любых неожиданных значений
		c.SetSameSite(http.SameSiteLaxMode)
	}
}

// setAuthCookies устанавливает access и refresh токены в httpOnly cookies
func (s *Server) setAuthCookies(c *gin.Context, accessToken, refreshToken string) {
	// Устанавливаем SameSite перед вызовом SetCookie
	s.setSameSiteMode(c)

	// Access token cookie
	c.SetCookie(
		s.config.Auth.CookieAccessName,
		accessToken,
		int(s.config.Auth.AccessTokenTTL.Seconds()),
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		s.config.Auth.CookieHttpOnly,
	)

	// Refresh token cookie
	c.SetCookie(
		s.config.Auth.CookieRefreshName,
		refreshToken,
		int(s.config.Auth.RefreshTokenTTL.Seconds()),
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		s.config.Auth.CookieHttpOnly,
	)

	// CSRF cookie (НЕ httpOnly, чтобы фронт мог прочитать и положить в header)
	s.setCsrfCookie(c)
}

func (s *Server) setCsrfCookie(c *gin.Context) {
	// Устанавливаем тот же SameSite что и при auth cookies
	s.setSameSiteMode(c)

	// 32 байта => 64 hex символа
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// Если не смогли сгенерить — лучше не падать, но и не открывать дыру:
		// просто не ставим cookie (запросы с изменениями будут 403 до появления csrf)
		return
	}
	token := hex.EncodeToString(b)

	// httpOnly=false
	c.SetCookie(
		csrfCookieName,
		token,
		int(s.config.Auth.RefreshTokenTTL.Seconds()),
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		false,
	)
}

// clearAuthCookies очищает auth cookies
func (s *Server) clearAuthCookies(c *gin.Context) {
	// Устанавливаем тот же SameSite что и при создании
	s.setSameSiteMode(c)

	c.SetCookie(
		s.config.Auth.CookieAccessName,
		"",
		-1,
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		s.config.Auth.CookieHttpOnly,
	)

	c.SetCookie(
		s.config.Auth.CookieRefreshName,
		"",
		-1,
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		s.config.Auth.CookieHttpOnly,
	)

	// CSRF cookie
	c.SetCookie(
		csrfCookieName,
		"",
		-1,
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		false,
	)
}

// parseIPAddress парсит IP адрес из строки
func parseIPAddress(ipStr string) *net.IP {
	// Пробуем разделить хост и порт (работает для IPv4 и IPv6)
	host, _, err := net.SplitHostPort(ipStr)
	if err == nil {
		ipStr = host
	}
	// Если SplitHostPort вернул ошибку, ipStr уже без порта

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}
	return &ip
}
