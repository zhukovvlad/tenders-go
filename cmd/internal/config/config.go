package config

import (
	"fmt"
	"sync"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/zhukovvlad/tenders-go/cmd/pkg/logging"
)

type ParserServiceConfig struct {
	URL string `yaml:"url" env-required:"true"`
}

type ServicesConfig struct {
	ParserService ParserServiceConfig `yaml:"parser_service"`
}

type AuthConfig struct {
	JWTSecret      string `yaml:"jwt_secret" env:"JWT_SECRET" env-required:"true"`
	AccessTTL      string `yaml:"access_ttl" env-default:"15m"`
	RefreshTTL     string `yaml:"refresh_ttl" env-default:"720h"` // 30 days
	CookieDomain   string `yaml:"cookie_domain" env-default:""`
	CookieSecure   bool   `yaml:"cookie_secure" env-default:"false"`
	CookieHttpOnly bool   `yaml:"cookie_http_only" env-default:"true"`
	CookieSameSite string `yaml:"cookie_same_site" env-default:"lax"` // strict, lax, none
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

	// Проверка Access TTL
	if _, err := time.ParseDuration(c.AccessTTL); err != nil {
		return fmt.Errorf("invalid access_ttl: %w", err)
	}

	// Проверка Refresh TTL
	if _, err := time.ParseDuration(c.RefreshTTL); err != nil {
		return fmt.Errorf("invalid refresh_ttl: %w", err)
	}

	// Проверка CookieSameSite
	validSameSite := map[string]bool{"strict": true, "lax": true, "none": true}
	if !validSameSite[c.CookieSameSite] {
		return fmt.Errorf("cookie_same_site must be one of: strict, lax, none (got: %s)", c.CookieSameSite)
	}

	// Warning для CookieSecure=false в production
	if !c.CookieSecure && !isDebug {
		logger := logging.GetLogger()
		logger.Warn("CookieSecure is false in production mode - authentication cookies are vulnerable to interception")
	}

	return nil
}

type Config struct {
	IsDebug *bool `yaml:"is_debug" env-required:"true"`
	Listen  struct {
		Type   string `yaml:"type" env-default:"port"`
		BindIP string `yaml:"bind_ip" env-default:"127.0.0.1"`
		Port   string `yaml:"port" env-default:"8080"`
	} `yaml:"listen"`
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
