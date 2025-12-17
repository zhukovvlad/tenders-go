package server

import (
	"errors"
	"net"
	"net/http"
	"strings"

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
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Получаем IP адрес клиента
	ipAddress := parseIPAddress(c.ClientIP())

	// Выполняем аутентификацию
	result, err := s.authService.Login(c.Request.Context(), req.Email, req.Password, ipAddress)
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

	// Получаем IP адрес
	ipAddress := parseIPAddress(c.ClientIP())

	// Обновляем токены
	result, err := s.authService.Refresh(c.Request.Context(), refreshToken, ipAddress)
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
	// Извлекаем access token из cookie
	accessToken, err := c.Cookie(s.config.Auth.CookieAccessName)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "access token not found"})
		return
	}

	// Валидируем токен
	claims, err := s.authService.ValidateAccessToken(accessToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired access token"})
		return
	}

	// Получаем полную информацию о пользователе
	user, err := s.store.GetUserByID(c.Request.Context(), claims.UserID)
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

// setAuthCookies устанавливает access и refresh токены в httpOnly cookies
func (s *Server) setAuthCookies(c *gin.Context, accessToken, refreshToken string) {
	// Access token cookie
	c.SetCookie(
		s.config.Auth.CookieAccessName,
		accessToken,
		int(s.config.Auth.AccessTokenTTL.Seconds()),
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		true, // httpOnly
	)

	// Refresh token cookie
	c.SetCookie(
		s.config.Auth.CookieRefreshName,
		refreshToken,
		int(s.config.Auth.RefreshTokenTTL.Seconds()),
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		true, // httpOnly
	)

	// SameSite устанавливается через SetSameSite
	// Gin не поддерживает его в SetCookie напрямую, но можно установить через заголовок
}

// clearAuthCookies очищает auth cookies
func (s *Server) clearAuthCookies(c *gin.Context) {
	c.SetCookie(
		s.config.Auth.CookieAccessName,
		"",
		-1,
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		true,
	)

	c.SetCookie(
		s.config.Auth.CookieRefreshName,
		"",
		-1,
		"/",
		s.config.Auth.CookieDomain,
		s.config.Auth.CookieSecure,
		true,
	)
}

// parseIPAddress парсит IP адрес из строки
func parseIPAddress(ipStr string) *net.IP {
	// Убираем порт если есть
	if idx := strings.LastIndex(ipStr, ":"); idx != -1 {
		ipStr = ipStr[:idx]
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}
	return &ip
}
