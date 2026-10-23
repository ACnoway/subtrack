package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddress       string
	DatabaseURL         string
	AdminUsername       string
	AdminPassword       string
	SessionSecret       []byte
	CookieSecure        bool
	SMTPHost            string
	SMTPPort            int
	SMTPUsername        string
	SMTPPassword        string
	SMTPFrom            string
	SMTPEncryption      string
	TelegramBotToken    string
	TelegramBotUsername string
	PublicURL           string
}

func LoadConfig() (Config, error) {
	secret := os.Getenv("SESSION_SECRET")
	if len(secret) < 32 {
		return Config{}, fmt.Errorf("SESSION_SECRET must contain at least 32 characters")
	}
	cfg := Config{
		ListenAddress:       envOr("LISTEN_ADDRESS", ":8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		AdminUsername:       envOr("ADMIN_USERNAME", "admin"),
		AdminPassword:       os.Getenv("ADMIN_PASSWORD"),
		SessionSecret:       []byte(secret),
		SMTPHost:            os.Getenv("SMTP_HOST"),
		SMTPUsername:        os.Getenv("SMTP_USERNAME"),
		SMTPPassword:        os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:            os.Getenv("SMTP_FROM"),
		SMTPEncryption:      envOr("SMTP_ENCRYPTION", "starttls"),
		TelegramBotToken:    os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramBotUsername: strings.TrimPrefix(os.Getenv("TELEGRAM_BOT_USERNAME"), "@"),
		PublicURL:           strings.TrimRight(os.Getenv("APP_PUBLIC_URL"), "/"),
	}
	port, err := strconv.Atoi(envOr("SMTP_PORT", "587"))
	if err != nil || port < 1 || port > 65535 {
		return Config{}, fmt.Errorf("SMTP_PORT must be between 1 and 65535")
	}
	cfg.SMTPPort = port
	if cfg.SMTPEncryption != "starttls" && cfg.SMTPEncryption != "tls" && cfg.SMTPEncryption != "none" {
		return Config{}, fmt.Errorf("SMTP_ENCRYPTION must be starttls, tls, or none")
	}
	if (cfg.SMTPUsername == "") != (cfg.SMTPPassword == "") {
		return Config{}, fmt.Errorf("SMTP_USERNAME and SMTP_PASSWORD must be configured together")
	}
	if cfg.PublicURL != "" {
		parsed, err := url.ParseRequestURI(cfg.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return Config{}, fmt.Errorf("APP_PUBLIC_URL must be an absolute HTTP or HTTPS URL")
		}
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
