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
	Port                 string        `yaml:"port" env:"GRPC_PORT" env-default:"50051" env-required:"true"`
	MaxIdle              time.Duration `yaml:"max_idle" env:"GRPC_MAX_IDLE" env-default:"30s"`
	Timeout              time.Duration `yaml:"timeout" env:"GRPC_TIMEOUT" env-default:"5s"`
	StartupDelay         time.Duration `yaml:"startup_delay" env:"GRPC_STARTUP_DELAY" env-default:"1s"`
	ShutdownPeriod       time.Duration `yaml:"shutdown_period" env:"GRPC_SHUTDOWN_PERIOD" env-default:"7s"`
	PublishRetryAttempts int           `yaml:"publish_retry_attempts" env:"GRPC_PUBLISH_RETRY_ATTEMPTS" env-default:"3"`
	PublishRetryBackoff  time.Duration `yaml:"publish_retry_backoff" env:"GRPC_PUBLISH_RETRY_BACKOFF" env-default:"100ms"`
}

// Add the Validate method to the Config struct
func (c *Config) Validate() error {
	// Validate AppConfig
	if c.App.Environment == "" {
		return fmt.Errorf("app environment is required")
	}
	if c.App.Name == "" {
		return fmt.Errorf("app name is required")
	}
	if c.App.Version == "" {
		return fmt.Errorf("app version is required")
	}

	// Validate GRPCServerConfig
	if c.GRPCServer.Port == "" {
		return fmt.Errorf("GRPC port is required")
	}
	if c.GRPCServer.MaxIdle <= 0 {
		return fmt.Errorf("GRPC max idle time must be positive")
	}
	if c.GRPCServer.Timeout <= 0 {
		return fmt.Errorf("GRPC timeout must be positive")
	}
	if c.GRPCServer.StartupDelay <= 0 {
		return fmt.Errorf("GRPC startup delay must be positive")
	}
	if c.GRPCServer.ShutdownPeriod <= 0 {
		return fmt.Errorf("shutdown period must be positive")
	}

	// Validate publish retry settings
	if c.GRPCServer.PublishRetryAttempts < 0 {
		return fmt.Errorf("publish retry attempts cannot be negative")
	}
	if c.GRPCServer.PublishRetryBackoff < 0 {
		return fmt.Errorf("publish retry backoff cannot be negative")
	}

	return nil
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
	if err := cleanenv.ReadConfig(configPath, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to read config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, err
}
