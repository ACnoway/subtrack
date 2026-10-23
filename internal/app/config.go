package app

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ListenAddress string
	DatabaseURL   string
	AdminUsername string
	AdminPassword string
	SessionSecret []byte
	CookieSecure  bool
}

func LoadConfig() (Config, error) {
	secret := os.Getenv("SESSION_SECRET")
	if len(secret) < 32 {
		return Config{}, fmt.Errorf("SESSION_SECRET must contain at least 32 characters")
	}
	cfg := Config{
		ListenAddress: envOr("LISTEN_ADDRESS", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		AdminUsername: envOr("ADMIN_USERNAME", "admin"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SessionSecret: []byte(secret),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.AdminPassword == "" {
		return Config{}, fmt.Errorf("ADMIN_PASSWORD is required")
	}
	secure, err := strconv.ParseBool(envOr("COOKIE_SECURE", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("COOKIE_SECURE must be true or false: %w", err)
	}
	cfg.CookieSecure = secure
	return cfg, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
