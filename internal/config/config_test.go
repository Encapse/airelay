package config_test

import (
	"os"
	"testing"

	"github.com/airelay/airelay/internal/config"
	"github.com/stretchr/testify/require"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://airelay:airelay@localhost:5432/airelay")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("JWT_SECRET", "supersecretjwtsecret")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "12345678901234567890123456789012") // 32 bytes
}

func TestLoad_Valid(t *testing.T) {
	setRequiredEnv(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "8081", cfg.ProxyPort)
	require.Equal(t, "8080", cfg.APIPort)
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	setRequiredEnv(t)
	os.Unsetenv("DATABASE_URL")
	_, err := config.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoad_MissingRedisURL(t *testing.T) {
	setRequiredEnv(t)
	os.Unsetenv("REDIS_URL")
	_, err := config.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "REDIS_URL")
}

func TestLoad_MissingJWTSecret(t *testing.T) {
	setRequiredEnv(t)
	os.Unsetenv("JWT_SECRET")
	_, err := config.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "JWT_SECRET")
}

func TestLoad_EncryptionKeyWrongLength(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "tooshort")
	_, err := config.Load()
	require.Error(t, err)
	require.Contains(t, err.Error(), "CREDENTIAL_ENCRYPTION_KEY")
}

func TestLoad_ProxyPortOverride(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PROXY_PORT", "9090")
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "9090", cfg.ProxyPort)
}
