package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/sqlc-dev/pqtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/zhukovvlad/tenders-go/cmd/internal/config"
	db "github.com/zhukovvlad/tenders-go/cmd/internal/db/sqlc"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrSessionNotFound    = errors.New("session not found or expired")
)

// dummyPasswordHash используется для защиты от timing attacks
// Генерируется при инициализации пакета
var dummyPasswordHash []byte

func init() {
	// Генерируем реальный bcrypt хеш для обеспечения полной вычислительной нагрузки
	var err error
	dummyPasswordHash, err = bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing-protection"), bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("failed to generate dummy hash: %v", err))
	}
}

// validateUserAgent обрезает User-Agent до безопасной длины (UTF-8 safe)
func validateUserAgent(ua string) string {
	const maxUserAgentLen = 255
	// Считаем руны (символы) а не байты для корректной работы с UTF-8
	runes := []rune(ua)
	if len(runes) > maxUserAgentLen {
		return string(runes[:maxUserAgentLen])
	}
	return ua
}

// ipToInet converts a net.IP pointer to pqtype.Inet for database storage.
func ipToInet(ip *net.IP) pqtype.Inet {
	if ip == nil {
		return pqtype.Inet{}
	}
	var bits int
	if ip.To4() != nil {
		bits = 32
	} else {
		bits = 128
	}
	return pqtype.Inet{
		IPNet: net.IPNet{
			IP:   *ip,
			Mask: net.CIDRMask(bits, bits),
		},
		Valid: true,
	}
}

// JWTClaims представляет payload JWT токена
type JWTClaims struct {
	UserID int64  `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// Service предоставляет методы для аутентификации
type Service struct {
	store  db.Store
	config *config.Config
	logger interface {
		Infof(format string, args ...interface{})
		Warnf(format string, args ...interface{})
		Errorf(format string, args ...interface{})
	}
}

// NewService создает новый auth service
func NewService(store db.Store, cfg *config.Config, logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}) *Service {
	return &Service{
		store:  store,
		config: cfg,
		logger: logger,
	}
}

// LoginResult содержит результат успешной аутентификации
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	User         db.User
}

// Login аутентифицирует пользователя по email и паролю
func (s *Service) Login(ctx context.Context, email, password string, ipAddress *net.IP, userAgent string) (*LoginResult, error) {
	// Нормализация email
	email = strings.ToLower(strings.TrimSpace(email))

	// Получение пользователя с паролем
	userAuth, err := s.store.GetUserAuthByEmail(ctx, email)
	if err != nil {
		if err == sql.ErrNoRows {
			// Выполняем dummy сравнение для защиты от timing attacks
			bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
			s.logger.Warnf("login attempt with non-existent email: %s", email)
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	// Проверка пароля (всегда первой, для защиты от timing attacks)
	if err := bcrypt.CompareHashAndPassword([]byte(userAuth.PasswordHash), []byte(password)); err != nil {
		s.logger.Warnf("failed login attempt for user %s (id: %d): invalid password", email, userAuth.ID)
		return nil, ErrInvalidCredentials
	}

	// Проверка что пользователь активен (после проверки пароля для одинакового времени выполнения)
	if !userAuth.IsActive {
		s.logger.Warnf("login attempt for inactive user %s (id: %d)", email, userAuth.ID)
		return nil, ErrInvalidCredentials
	}

	// Генерация refresh token
	refreshToken, refreshHash, err := generateRefreshToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	// Валидация и обрезка User-Agent
	userAgent = validateUserAgent(userAgent)

	// Создание сессии + обновление last_login_at в одной транзакции
	err = s.store.ExecTx(ctx, func(q *db.Queries) error {
		sessionParams := db.CreateUserSessionParams{
			UserID:           userAuth.ID,
			RefreshTokenHash: refreshHash,
			UserAgent: sql.NullString{
				String: userAgent,
				Valid:  userAgent != "",
			},
			IpAddress: ipToInet(ipAddress),
			ExpiresAt: time.Now().Add(s.config.Auth.RefreshTokenTTL),
		}

		_, err := q.CreateUserSession(ctx, sessionParams)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		// Обновляем last_login_at (в проекте уже есть sqlc-запрос UpdateUserLastLogin)
		if err := q.UpdateUserLastLogin(ctx, userAuth.ID); err != nil {
			return fmt.Errorf("failed to update last_login_at: %w", err)
		}

		return nil
	})
	if err != nil {
		s.logger.Errorf("failed to create session for user %s (id: %d): %v", email, userAuth.ID, err)
		return nil, err
	}

	s.logger.Infof("successful login for user %s (id: %d)", email, userAuth.ID)

	// Генерация access token
	accessToken, err := s.generateAccessToken(userAuth.ID, userAuth.Role)
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	return &LoginResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: db.User{
			ID:        userAuth.ID,
			Email:     userAuth.Email,
			Role:      userAuth.Role,
			CreatedAt: userAuth.CreatedAt,
			UpdatedAt: userAuth.UpdatedAt,
		},
	}, nil
}

// RefreshResult содержит новые токены после обновления
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	UserID       int64
	Role         string
}

// Refresh обновляет access token используя refresh token (в транзакции)
func (s *Service) Refresh(ctx context.Context, refreshToken string, ipAddress *net.IP, userAgent string) (*RefreshResult, error) {
	if err := validateRefreshTokenFormat(refreshToken); err != nil {
		return nil, ErrInvalidToken
	}

	refreshHash := hashRefreshToken(refreshToken)

	var result RefreshResult

	// Выполняем в транзакции
	err := s.store.ExecTx(ctx, func(q *db.Queries) error {
		// Получаем активную сессию с блокировкой
		session, err := q.GetActiveSessionByRefreshHashForUpdate(ctx, refreshHash)
		if err != nil {
			if err == sql.ErrNoRows {
				return ErrSessionNotFound
			}
			return fmt.Errorf("failed to get session: %w", err)
		}

		// Проверяем срок действия
		if time.Now().After(session.ExpiresAt) {
			return ErrSessionNotFound
		}

		// Отзываем старую сессию
		if err := q.RevokeSessionByID(ctx, session.ID); err != nil {
			return fmt.Errorf("failed to revoke old session: %w", err)
		}

		// Генерируем новый refresh token
		newRefreshToken, newRefreshHash, err := generateRefreshToken()
		if err != nil {
			return fmt.Errorf("failed to generate refresh token: %w", err)
		}

		// Валидация и обрезка User-Agent
		userAgent = validateUserAgent(userAgent)

		// Создаем новую сессию
		sessionParams := db.CreateUserSessionParams{
			UserID:           session.UserID,
			RefreshTokenHash: newRefreshHash,
			UserAgent: sql.NullString{
				String: userAgent,
				Valid:  userAgent != "",
			},
			IpAddress: ipToInet(ipAddress),
			ExpiresAt: time.Now().Add(s.config.Auth.RefreshTokenTTL),
		}

		_, err = q.CreateUserSession(ctx, sessionParams)
		if err != nil {
			return fmt.Errorf("failed to create new session: %w", err)
		}

		// Получаем информацию о пользователе для role
		user, err := q.GetUserByID(ctx, session.UserID)
		if err != nil {
			return fmt.Errorf("failed to get user: %w", err)
		}

		// Генерируем новый access token
		accessToken, err := s.generateAccessToken(user.ID, user.Role)
		if err != nil {
			return fmt.Errorf("failed to generate access token: %w", err)
		}

		result = RefreshResult{
			AccessToken:  accessToken,
			RefreshToken: newRefreshToken,
			UserID:       user.ID,
			Role:         user.Role,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Logout отзывает refresh token (завершает сессию)
func (s *Service) Logout(ctx context.Context, refreshToken string) error {
	if err := validateRefreshTokenFormat(refreshToken); err != nil {
		return ErrInvalidToken
	}

	refreshHash := hashRefreshToken(refreshToken)

	err := s.store.RevokeSessionByRefreshHash(ctx, refreshHash)
	if err != nil {
		return fmt.Errorf("failed to revoke session: %w", err)
	}

	// :exec не возвращает sql.ErrNoRows, поэтому nil означает успех
	// (даже если не было обновлено ни одной строки)
	return nil
}

// ValidateAccessToken валидирует JWT access token и возвращает claims
func (s *Service) ValidateAccessToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Проверяем алгоритм подписи (строго HS256)
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.Auth.JWTSecret), nil
	})

	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// generateAccessToken создает JWT access token
func (s *Service) generateAccessToken(userID int64, role string) (string, error) {
	now := time.Now()
	claims := JWTClaims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.Auth.AccessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "tenders-go",
			Subject:   fmt.Sprintf("%d", userID),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.Auth.JWTSecret))
}

// generateRefreshToken генерирует случайный refresh token и его SHA-256 хеш
func generateRefreshToken() (token string, hash string, err error) {
	// Генерируем 32 случайных байта
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}

	// Токен в hex формате (64 символа)
	token = hex.EncodeToString(bytes)

	// SHA-256 хеш для хранения в БД
	hash = hashRefreshToken(token)

	return token, hash, nil
}

// hashRefreshToken вычисляет SHA-256 хеш refresh token
func hashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// validateRefreshTokenFormat проверяет формат refresh token (hex 32 bytes -> 64 символа).
// Это позволяет быстро отсеивать мусорные/битые cookie и корректно возвращать ErrInvalidToken.
func validateRefreshTokenFormat(token string) error {
	// Ожидаем 32 байта в hex => 64 символа
	if len(token) != 64 {
		return ErrInvalidToken
	}
	// Должен быть валидный hex
	if _, err := hex.DecodeString(token); err != nil {
		return ErrInvalidToken
	}
	return nil
}
