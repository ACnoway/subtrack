ALTER TABLE subscriptions
    ADD COLUMN custom_cycle_days INTEGER CHECK (custom_cycle_days IS NULL OR custom_cycle_days BETWEEN 1 AND 36500);

CREATE TABLE renewal_records (
    id BIGSERIAL PRIMARY KEY,
    subscription_id BIGINT REFERENCES subscriptions(id) ON DELETE SET NULL,
    subscription_name TEXT NOT NULL,
    renewed_at DATE NOT NULL,
    previous_expires_at DATE NOT NULL,
    new_expires_at DATE NOT NULL,
    amount NUMERIC(12, 2) CHECK (amount IS NULL OR amount >= 0),
    currency CHAR(3) NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX renewal_records_date_idx ON renewal_records (renewed_at DESC, id DESC);
CREATE INDEX renewal_records_subscription_idx ON renewal_records (subscription_id, renewed_at DESC);
