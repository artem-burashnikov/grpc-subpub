package config

import (
	"fmt"
	"os"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	App        AppConfig        `yaml:"app"`
	GRPCServer GRPCServerConfig `yaml:"grpc_server"`
}

type AppConfig struct {
	Environment string `yaml:"environment" env:"APP_ENV" env-default:"dev" env-required:"true"`
	Name        string `yaml:"name" env:"APP_NAME" env-required:"true"`
	Version     string `yaml:"version" env:"APP_VERSION" env-required:"true"`
}

type GRPCServerConfig struct {
	Port           string `yaml:"port" env:"GRPC_PORT" env-default:"50051" env-required:"true"`
	MaxIdle        time.Duration
	Timeout        time.Duration `yaml:"timeout" env:"GRPC_TIMEOUT" env-default:"5s"`
	ShutdownPeriod time.Duration `yaml:"shutdown_period" env:"GRPC_SHUTDOWN_PERIOD" env-default:"7s"`
}

func Load(configPath string) (Config, error) {
	if configPath == "" {
		return Config{}, fmt.Errorf("CONFIG_PATH environment variable must be set")
	}

	fileInfo, err := os.Stat(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, fmt.Errorf("Config file does not exist: %q", configPath)
		}
		return Config{}, err
	}
	if fileInfo.IsDir() {
		return Config{}, fmt.Errorf("config path %q is a directory, not a file", configPath)
	}

	var cfg Config
	err = cleanenv.ReadConfig(configPath, &cfg)

	return cfg, err
}
