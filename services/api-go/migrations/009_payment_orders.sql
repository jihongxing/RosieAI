CREATE TABLE IF NOT EXISTS payment_orders (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    order_no TEXT NOT NULL UNIQUE,
    order_type TEXT NOT NULL DEFAULT 'renewal',
    plan_code TEXT NOT NULL DEFAULT 'pilot_basic',
    add_on_code TEXT,
    amount_cents INTEGER NOT NULL,
    currency TEXT NOT NULL DEFAULT 'CNY',
    status TEXT NOT NULL DEFAULT 'pending',
    provider TEXT NOT NULL DEFAULT 'wechat_pay',
    provider_trade_no TEXT,
    prepay_id TEXT,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_payment_orders_merchant
    ON payment_orders(merchant_id, id DESC);

CREATE INDEX IF NOT EXISTS idx_payment_orders_status
    ON payment_orders(status, id DESC);
