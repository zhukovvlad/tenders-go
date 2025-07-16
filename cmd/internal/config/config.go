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

type Config struct {
	IsDebug *bool `yaml:"is_debug" env-required:"true"`
	Listen  struct {
		Type   string `yaml:"type" env-default:"port"`
		BindIP string `yaml:"bind_ip" env-default:"127.0.0.1"`
		Port   string `yaml:"port" env-default:"8080"`
	} `yaml:"listen"`
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
