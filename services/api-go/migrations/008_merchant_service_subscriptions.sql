CREATE TABLE IF NOT EXISTS merchant_service_subscriptions (
    merchant_id TEXT PRIMARY KEY REFERENCES merchants(merchant_id),
    plan_code TEXT NOT NULL DEFAULT 'pilot_basic',
    status TEXT NOT NULL DEFAULT 'not_started',
    trial_started_at TIMESTAMPTZ,
    trial_ends_at TIMESTAMPTZ,
    current_period_ends_at TIMESTAMPTZ,
    activated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_merchant_service_subscriptions_status
    ON merchant_service_subscriptions(status, current_period_ends_at);
