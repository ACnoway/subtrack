CREATE TABLE integration_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    smtp_host TEXT NOT NULL DEFAULT '',
    smtp_port INTEGER NOT NULL DEFAULT 587 CHECK (smtp_port BETWEEN 1 AND 65535),
    smtp_username TEXT NOT NULL DEFAULT '',
    smtp_password_ciphertext TEXT NOT NULL DEFAULT '',
    smtp_from TEXT NOT NULL DEFAULT '',
    smtp_encryption TEXT NOT NULL DEFAULT 'starttls' CHECK (smtp_encryption IN ('starttls', 'tls', 'none')),
    telegram_bot_token_ciphertext TEXT NOT NULL DEFAULT '',
    telegram_bot_username TEXT NOT NULL DEFAULT '',
    initialized BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO integration_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
