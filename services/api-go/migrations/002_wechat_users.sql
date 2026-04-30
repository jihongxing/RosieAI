CREATE TABLE IF NOT EXISTS app_users (
    id BIGSERIAL PRIMARY KEY,
    openid TEXT NOT NULL UNIQUE,
    unionid TEXT,
    session_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_app_users_unionid ON app_users(unionid);

CREATE TABLE IF NOT EXISTS merchant_user_bindings (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    user_id BIGINT NOT NULL REFERENCES app_users(id),
    role TEXT NOT NULL DEFAULT 'owner',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (merchant_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_merchant_user_bindings_merchant ON merchant_user_bindings(merchant_id, enabled, id);
