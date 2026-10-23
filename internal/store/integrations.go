package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

type IntegrationSettings struct {
	SMTPHost            string
	SMTPPort            int
	SMTPUsername        string
	SMTPPassword        string
	SMTPFrom            string
	SMTPEncryption      string
	TelegramBotToken    string
	TelegramBotUsername string
}

func (s *Store) EnsureIntegrationSettings(ctx context.Context, legacy IntegrationSettings) error {
	password, err := s.encryptSecret(legacy.SMTPPassword)
	if err != nil {
		return err
	}
	token, err := s.encryptSecret(legacy.TelegramBotToken)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin integration settings initialization: %w", err)
	}
	defer tx.Rollback()
	var initialized bool
	if err := tx.QueryRowContext(ctx, `SELECT initialized FROM integration_settings WHERE id=1 FOR UPDATE`).Scan(&initialized); err != nil {
		return fmt.Errorf("read integration settings initialization: %w", err)
	}
	if initialized {
		return tx.Commit()
	}
	if legacy.SMTPPort == 0 {
		legacy.SMTPPort = 587
	}
	if legacy.SMTPEncryption == "" {
		legacy.SMTPEncryption = "starttls"
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE integration_settings
		SET smtp_host=$1, smtp_port=$2, smtp_username=$3, smtp_password_ciphertext=$4,
		    smtp_from=$5, smtp_encryption=$6, telegram_bot_token_ciphertext=$7,
		    telegram_bot_username=$8, initialized=true, updated_at=now()
		WHERE id=1`, strings.TrimSpace(legacy.SMTPHost), legacy.SMTPPort,
		strings.TrimSpace(legacy.SMTPUsername), password, strings.TrimSpace(legacy.SMTPFrom),
		legacy.SMTPEncryption, token, strings.TrimPrefix(strings.TrimSpace(legacy.TelegramBotUsername), "@"))
	if err != nil {
		return fmt.Errorf("import legacy integration settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit integration settings initialization: %w", err)
	}
	return nil
}

func (s *Store) GetIntegrationSettings(ctx context.Context) (IntegrationSettings, error) {
	var settings IntegrationSettings
	var password, token string
	err := s.db.QueryRowContext(ctx, `
		SELECT smtp_host, smtp_port, smtp_username, smtp_password_ciphertext,
		       smtp_from, smtp_encryption, telegram_bot_token_ciphertext, telegram_bot_username
		FROM integration_settings WHERE id=1`).Scan(
		&settings.SMTPHost, &settings.SMTPPort, &settings.SMTPUsername, &password,
		&settings.SMTPFrom, &settings.SMTPEncryption, &token, &settings.TelegramBotUsername)
	if err != nil {
		return IntegrationSettings{}, fmt.Errorf("read integration settings: %w", err)
	}
	settings.SMTPPassword, err = s.decryptSecret(password)
	if err != nil {
		return IntegrationSettings{}, fmt.Errorf("decrypt SMTP password: %w", err)
	}
	settings.TelegramBotToken, err = s.decryptSecret(token)
	if err != nil {
		return IntegrationSettings{}, fmt.Errorf("decrypt Telegram bot token: %w", err)
	}
	return settings, nil
}

func (s *Store) SaveNotificationConfiguration(ctx context.Context, notification NotificationSettings, rules ReminderRules, integrations IntegrationSettings) error {
	password, err := s.encryptSecret(integrations.SMTPPassword)
	if err != nil {
		return err
	}
	token, err := s.encryptSecret(integrations.TelegramBotToken)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin notification configuration update: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
		UPDATE notification_settings
		SET email=$1, email_enabled=$2, telegram_enabled=$3, timezone=$4,
		    delivery_time=$5::time, updated_at=now()
		WHERE id=1`, strings.TrimSpace(notification.Email), notification.EmailEnabled,
		notification.TelegramEnabled, notification.Timezone, notification.DeliveryTime); err != nil {
		return fmt.Errorf("save notification settings: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM reminder_rules`); err != nil {
		return fmt.Errorf("replace reminder rules: %w", err)
	}
	insertRule := `INSERT INTO reminder_rules (scope, days_before) VALUES ($1,$2)`
	for _, days := range rules.Default {
		if _, err := tx.ExecContext(ctx, insertRule, "default", days); err != nil {
			return fmt.Errorf("save default reminder rule: %w", err)
		}
	}
	for _, days := range rules.Important {
		if _, err := tx.ExecContext(ctx, insertRule, "important", days); err != nil {
			return fmt.Errorf("save important reminder rule: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE integration_settings SET smtp_host=$1, smtp_port=$2, smtp_username=$3,
		    smtp_password_ciphertext=$4, smtp_from=$5, smtp_encryption=$6,
		    telegram_bot_token_ciphertext=$7, telegram_bot_username=$8,
		    initialized=true, updated_at=now() WHERE id=1`,
		strings.TrimSpace(integrations.SMTPHost), integrations.SMTPPort,
		strings.TrimSpace(integrations.SMTPUsername), password, strings.TrimSpace(integrations.SMTPFrom),
		integrations.SMTPEncryption, token,
		strings.TrimPrefix(strings.TrimSpace(integrations.TelegramBotUsername), "@")); err != nil {
		return fmt.Errorf("save integration settings: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit notification configuration: %w", err)
	}
	return nil
}

func (s *Store) encryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	block, err := aes.NewCipher(s.integrationKey())
	if err != nil {
		return "", fmt.Errorf("create integration settings cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create integration settings encryption: %w", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create integration settings nonce: %w", err)
	}
	ciphertext := aead.Seal(nonce, nonce, []byte(value), []byte("subtrack-integration-settings-v1"))
	return base64.RawStdEncoding.EncodeToString(ciphertext), nil
}

func (s *Store) decryptSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("decode encrypted setting: %w", err)
	}
	block, err := aes.NewCipher(s.integrationKey())
	if err != nil {
		return "", fmt.Errorf("create integration settings cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create integration settings encryption: %w", err)
	}
	if len(ciphertext) < aead.NonceSize() {
		return "", fmt.Errorf("encrypted setting is truncated")
	}
	nonce, ciphertext := ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte("subtrack-integration-settings-v1"))
	if err != nil {
		return "", fmt.Errorf("decrypt encrypted setting: %w", err)
	}
	return string(plaintext), nil
}

func (s *Store) integrationKey() []byte {
	mac := hmac.New(sha256.New, s.encryptionSecret)
	_, _ = mac.Write([]byte("subtrack-integration-settings-v1"))
	return mac.Sum(nil)
}
