CREATE TABLE notification_settings (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    email TEXT NOT NULL DEFAULT '',
    email_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    telegram_chat_id TEXT NOT NULL DEFAULT '',
    telegram_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    delivery_time TIME NOT NULL DEFAULT '09:00',
    telegram_bind_token_hash TEXT,
    telegram_bind_expires_at TIMESTAMPTZ,
    telegram_update_offset BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO notification_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

CREATE TABLE notification_worker_state (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_seen_at TIMESTAMPTZ NOT NULL
);

INSERT INTO notification_worker_state (id, last_seen_at) VALUES (1, now()) ON CONFLICT (id) DO NOTHING;

CREATE TABLE reminder_rules (
    id BIGSERIAL PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('default', 'important')),
    days_before INTEGER NOT NULL CHECK (days_before BETWEEN -365 AND 365),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    UNIQUE (scope, days_before)
);

INSERT INTO reminder_rules (scope, days_before) VALUES
    ('default', 30), ('default', 14), ('default', 7), ('default', 3), ('default', 1), ('default', 0),
    ('important', 60)
ON CONFLICT (scope, days_before) DO NOTHING;

CREATE TABLE notification_jobs (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT REFERENCES subscriptions(id) ON DELETE SET NULL,
    subscription_name TEXT NOT NULL,
    target_date DATE NOT NULL,
    reminder_offset INTEGER NOT NULL,
    channel TEXT NOT NULL CHECK (channel IN ('email', 'telegram')),
    scheduled_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed', 'cancelled')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    last_error TEXT NOT NULL DEFAULT '',
    sent_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subscription_id, target_date, reminder_offset, channel)
);

CREATE INDEX notification_jobs_due_idx ON notification_jobs (status, scheduled_at) WHERE status IN ('pending', 'sending');
CREATE INDEX notification_jobs_subscription_idx ON notification_jobs (subscription_id, status);

CREATE TABLE notification_logs (
    id BIGSERIAL PRIMARY KEY,
    job_id BIGINT REFERENCES notification_jobs(id) ON DELETE SET NULL,
    subscription_id BIGINT REFERENCES subscriptions(id) ON DELETE SET NULL,
    subscription_name TEXT NOT NULL DEFAULT '',
    reminder_offset INTEGER NOT NULL DEFAULT 0,
    channel TEXT NOT NULL CHECK (channel IN ('email', 'telegram')),
    scheduled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    result TEXT NOT NULL CHECK (result IN ('sent', 'retrying', 'failed', 'test_sent', 'test_failed')),
    attempt_count INTEGER NOT NULL DEFAULT 1,
    error_message TEXT NOT NULL DEFAULT ''
);

CREATE INDEX notification_logs_history_idx ON notification_logs (attempted_at DESC, id DESC);
