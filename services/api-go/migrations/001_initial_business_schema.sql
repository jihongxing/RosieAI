-- Rosie formal business backend schema.
-- Python MVP remains the reference implementation for validated behavior;
-- Go owns the formal business data model from this point forward.

CREATE TABLE IF NOT EXISTS merchants (
    merchant_id TEXT PRIMARY KEY,
    merchant_name TEXT NOT NULL,
    access_number TEXT NOT NULL UNIQUE,
    original_number TEXT,
    transfer_phone TEXT,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS calls (
    id BIGSERIAL PRIMARY KEY,
    call_sid TEXT NOT NULL UNIQUE,
    call_id TEXT,
    merchant_id TEXT REFERENCES merchants(merchant_id),
    from_number TEXT,
    to_number TEXT,
    call_status TEXT,
    direction TEXT,
    raw_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_calls_merchant ON calls(merchant_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_calls_to_number ON calls(to_number);

CREATE TABLE IF NOT EXISTS call_transcripts (
    id BIGSERIAL PRIMARY KEY,
    call_sid TEXT NOT NULL UNIQUE REFERENCES calls(call_sid),
    merchant_id TEXT REFERENCES merchants(merchant_id),
    transcript TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'manual',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS call_summaries (
    id BIGSERIAL PRIMARY KEY,
    call_sid TEXT NOT NULL UNIQUE REFERENCES calls(call_sid),
    merchant_id TEXT REFERENCES merchants(merchant_id),
    summary TEXT,
    customer_name TEXT,
    customer_phone TEXT,
    intent TEXT,
    appointment_time TEXT,
    service TEXT,
    priority TEXT NOT NULL DEFAULT 'normal',
    need_human_followup BOOLEAN NOT NULL DEFAULT FALSE,
    raw_result JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_call_summaries_merchant ON call_summaries(merchant_id, id DESC);

CREATE TABLE IF NOT EXISTS inbox_items (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    call_sid TEXT NOT NULL UNIQUE REFERENCES calls(call_sid),
    item_type TEXT NOT NULL DEFAULT 'call_summary',
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    priority TEXT NOT NULL DEFAULT 'normal',
    status TEXT NOT NULL DEFAULT 'new',
    need_human_followup BOOLEAN NOT NULL DEFAULT FALSE,
    digest_status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inbox_items_merchant ON inbox_items(merchant_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_inbox_items_digest ON inbox_items(merchant_id, digest_status, id);

CREATE TABLE IF NOT EXISTS digests (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    digest_type TEXT NOT NULL DEFAULT 'daily',
    item_count INTEGER NOT NULL DEFAULT 0,
    urgent_count INTEGER NOT NULL DEFAULT 0,
    followup_count INTEGER NOT NULL DEFAULT 0,
    spam_count INTEGER NOT NULL DEFAULT 0,
    digest_text TEXT NOT NULL,
    item_ids BIGINT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'generated',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_digests_merchant ON digests(merchant_id, id DESC);

CREATE TABLE IF NOT EXISTS notification_preferences (
    merchant_id TEXT PRIMARY KEY REFERENCES merchants(merchant_id),
    digest_mode TEXT NOT NULL DEFAULT 'daily',
    digest_times TEXT[] NOT NULL DEFAULT ARRAY['20:00'],
    realtime_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    urgent_realtime_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    team_wecom_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    sms_fallback_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    quiet_hours_start TEXT,
    quiet_hours_end TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS notification_logs (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    channel TEXT NOT NULL,
    message_type TEXT NOT NULL,
    target TEXT,
    subject TEXT,
    body TEXT NOT NULL,
    related_digest_id BIGINT REFERENCES digests(id),
    related_inbox_item_id BIGINT REFERENCES inbox_items(id),
    idempotency_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notification_logs_merchant ON notification_logs(merchant_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_notification_logs_status ON notification_logs(status, id);
