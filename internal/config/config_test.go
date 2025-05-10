package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidConfigFile(t *testing.T) {
	require := require.New(t)

	configContent := `
app:
  environment: test
  name: test-app
  version: 0.1.0
grpc_server:
  port: "50051"
  max_idle: 30s
  timeout: 5s
  startup_delay: 1s
  shutdown_period: 7s
  publish_retry_attempts: 3
  publish_retry_backoff: 100ms
`
	tmpFile, err := os.CreateTemp("", "config*.yaml")
	require.Nil(err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte(configContent))
	require.Nil(err)
	require.NoError(tmpFile.Close())

	cfg, err := Load(tmpFile.Name())
	require.Nil(err)

	assert := assert.New(t)

	assert.Equal("test", cfg.App.Environment)
	assert.Equal("test-app", cfg.App.Name)
	assert.Equal("0.1.0", cfg.App.Version)
	assert.Equal("50051", cfg.GRPCServer.Port)
	assert.Equal(30*time.Second, cfg.GRPCServer.MaxIdle)
	assert.Equal(5*time.Second, cfg.GRPCServer.Timeout)
	assert.Equal(1*time.Second, cfg.GRPCServer.StartupDelay)
	assert.Equal(7*time.Second, cfg.GRPCServer.ShutdownPeriod)
	assert.Equal(3, cfg.GRPCServer.PublishRetryAttempts)
	assert.Equal(100*time.Millisecond, cfg.GRPCServer.PublishRetryBackoff)
}

func TestLoad_InvalidConfigFile(t *testing.T) {
	configContent := `
app:
  environment: test
  name: test-app
  version: 0.1.0
grpc_server:
  port: ""
  max_idle: -30s
  timeout: 0s
`
	tmpFile, err := os.CreateTemp("", "config*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write([]byte(configContent))
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	_, err = Load(tmpFile.Name())
	require.Error(t, err)
}

func TestLoad_MissingConfigFile(t *testing.T) {
	_, err := Load("nonexistent.yaml")
	require.Error(t, err)
}

func TestLoad_ConfigPathIsDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "configdir")
	require.Nil(t, err)
	defer os.RemoveAll(tmpDir)

	_, err = Load(tmpDir)
	require.Error(t, err)
}

func TestValidate_InvalidConfig(t *testing.T) {
	cfg := Config{
		App: AppConfig{
			Environment: "",
			Name:        "",
			Version:     "",
		},
		GRPCServer: GRPCServerConfig{
			Port:                 "",
			MaxIdle:              -1 * time.Second,
			Timeout:              0,
			StartupDelay:         -1 * time.Second,
			ShutdownPeriod:       -1 * time.Second,
			PublishRetryAttempts: -1,
			PublishRetryBackoff:  -1 * time.Millisecond,
		},
	}

	err := cfg.Validate()
	require.Error(t, err)
}
