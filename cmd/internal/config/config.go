package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

// validCookieSameSiteValues определяет допустимые значения для атрибута SameSite cookie.
// SameSite защищает от CSRF атак, контролируя когда браузер отправляет cookies:
//   - "strict": cookie отправляется только при запросах с того же сайта (максимальная защита)
//   - "lax": cookie отправляется при переходах по ссылкам, но не при POST запросах с других сайтов (баланс)
//   - "none": cookie отправляется всегда, даже с других сайтов (требует Secure=true, только HTTPS)
var validCookieSameSiteValues = map[string]bool{
	"strict": true,
	"lax":    true,
	"none":   true,
}

type ParserServiceConfig struct {
	URL string `yaml:"url" env-required:"true"`
}

type ServicesConfig struct {
	ParserService ParserServiceConfig `yaml:"parser_service"`
}

type AuthConfig struct {
	JWTSecret         string `yaml:"jwt_secret" env:"JWT_SECRET" env-required:"true"`
	AccessTTL         string `yaml:"access_ttl" env-default:"15m"`
	RefreshTTL        string `yaml:"refresh_ttl" env-default:"720h"` // 30 days
	CookieAccessName  string `yaml:"cookie_access_name" env-default:"access_token"`
	CookieRefreshName string `yaml:"cookie_refresh_name" env-default:"refresh_token"`
	CookieDomain      string `yaml:"cookie_domain" env-default:""`
	CookieSecure      bool   `yaml:"cookie_secure" env-default:"false"`
	CookieHttpOnly    bool   `yaml:"cookie_http_only" env-default:"true"`
	CookieSameSite    string `yaml:"cookie_same_site" env-default:"lax"` // strict, lax, none

	// Парсированные значения (заполняются после Validate)
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Validate проверяет корректность настроек auth конфигурации
func (c *AuthConfig) Validate(isDebug bool) error {
	// Проверка JWT secret
	if c.JWTSecret == "" {
		return fmt.Errorf("jwt_secret is required")
	}
	if len(c.JWTSecret) < 32 {
		return fmt.Errorf("jwt_secret must be at least 32 characters (current: %d)", len(c.JWTSecret))
	}

	// Проверка и парсинг Access TTL
	accessTTL, err := time.ParseDuration(c.AccessTTL)
	if err != nil {
		return fmt.Errorf("invalid access_ttl: %w", err)
	}
	c.AccessTokenTTL = accessTTL

	// Проверка и парсинг Refresh TTL
	refreshTTL, err := time.ParseDuration(c.RefreshTTL)
	if err != nil {
		return fmt.Errorf("invalid refresh_ttl: %w", err)
	}
	c.RefreshTokenTTL = refreshTTL

	// Проверка что refresh TTL больше access TTL
	if c.RefreshTokenTTL <= c.AccessTokenTTL {
		return fmt.Errorf("refresh_ttl must be greater than access_ttl")
	}

	// Проверка CookieSameSite
	if !validCookieSameSiteValues[c.CookieSameSite] {
		return fmt.Errorf("cookie_same_site must be one of: strict, lax, none (got: %s)", c.CookieSameSite)
	}

	// Warning для CookieSecure=false в production
	if !c.CookieSecure && !isDebug {
		logger := logging.GetLogger()
		logger.Warn("CookieSecure is false in production mode - authentication cookies are vulnerable to interception")
	}

	return nil
}

type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins" env:"CORS_ALLOWED_ORIGINS" env-separator:","`
}

type Config struct {
	IsDebug *bool `yaml:"is_debug" env-required:"true"`
	Listen  struct {
		Type   string `yaml:"type" env-default:"port"`
		BindIP string `yaml:"bind_ip" env-default:"127.0.0.1"`
		Port   string `yaml:"port" env-default:"8080"`
	} `yaml:"listen"`
	Database struct {
		Driver string `yaml:"driver" env:"DB_DRIVER" env-default:"postgres"`
		Source string `yaml:"source" env:"DB_SOURCE" env-required:"true"`
	} `yaml:"database"`
	CORS     CORSConfig     `yaml:"cors"`
	Auth     AuthConfig     `yaml:"auth"`
	Services ServicesConfig `yaml:"services"`
}

var instance *Config
var once sync.Once

func GetConfig() *Config {
	once.Do(func() {
		logger := logging.GetLogger()
		logger.Info("read application configuration")
		instance = &Config{}
		if err := cleanenv.ReadConfig("./cmd/config/config.yml", instance); err != nil {
			help, _ := cleanenv.GetDescription(instance, nil)
			logger.Info(help)
			logger.Fatal(err)
		}

		// Валидация auth конфигурации
		isDebug := instance.IsDebug != nil && *instance.IsDebug
		if err := instance.Auth.Validate(isDebug); err != nil {
			logger.Fatal("invalid auth configuration: ", err)
		}
	})

	return instance
}
