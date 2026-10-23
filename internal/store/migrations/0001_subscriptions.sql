CREATE TABLE subscriptions (
    id BIGSERIAL PRIMARY KEY,
    owner_id TEXT NOT NULL DEFAULT 'admin',
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    category TEXT NOT NULL CHECK (category IN ('server', 'domain', 'cloud', 'software', 'api', 'certificate', 'other')),
    provider TEXT NOT NULL DEFAULT '',
    account_label TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    start_date DATE,
    expires_at DATE NOT NULL,
    next_billing_at DATE,
    renewal_cycle TEXT NOT NULL CHECK (renewal_cycle IN ('monthly', 'quarterly', 'semiannual', 'annual', 'custom', 'one_time')),
    renewal_price NUMERIC(12, 2) CHECK (renewal_price IS NULL OR renewal_price >= 0),
    currency CHAR(3) NOT NULL DEFAULT 'USD' CHECK (currency ~ '^[A-Z]{3}$'),
    auto_renew BOOLEAN NOT NULL DEFAULT FALSE,
    renewal_url TEXT NOT NULL DEFAULT '',
    priority SMALLINT NOT NULL DEFAULT 2 CHECK (priority BETWEEN 1 AND 3),
    tags TEXT NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    archived BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX subscriptions_owner_expiry_idx ON subscriptions (owner_id, archived, expires_at);
CREATE INDEX subscriptions_category_idx ON subscriptions (category);
