package config

import (
	"fmt"
	"os"
)

type Config struct {
	DatabaseURL             string
	RedisURL                string
	ProxyPort               string
	APIPort                 string
	JWTSecret               string
	CredentialEncryptionKey string
	OpenAIKey               string
	AnthropicKey            string
	GoogleKey               string
	StripeSecretKey         string
	StripeWebhookSecret     string
	ResendAPIKey            string
	PostHogKey              string
}

func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		RedisURL:                os.Getenv("REDIS_URL"),
		ProxyPort:               getEnvOrDefault("PROXY_PORT", "8081"),
		APIPort:                 getEnvOrDefault("API_PORT", "8080"),
		JWTSecret:               os.Getenv("JWT_SECRET"),
		CredentialEncryptionKey: os.Getenv("CREDENTIAL_ENCRYPTION_KEY"),
		OpenAIKey:               os.Getenv("OPENAI_API_KEY"),
		AnthropicKey:            os.Getenv("ANTHROPIC_API_KEY"),
		GoogleKey:               os.Getenv("GOOGLE_API_KEY"),
		StripeSecretKey:         os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:     os.Getenv("STRIPE_WEBHOOK_SECRET"),
		ResendAPIKey:            os.Getenv("RESEND_API_KEY"),
		PostHogKey:              os.Getenv("POSTHOG_API_KEY"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if len(cfg.CredentialEncryptionKey) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be exactly 32 bytes, got %d", len(cfg.CredentialEncryptionKey))
	}
	return cfg, nil
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
