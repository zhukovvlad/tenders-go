package config

import (
	"sync"

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
	})

	return instance
}
