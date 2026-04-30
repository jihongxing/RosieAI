CREATE TABLE IF NOT EXISTS callback_requests (
    id BIGSERIAL PRIMARY KEY,
    merchant_id TEXT NOT NULL REFERENCES merchants(merchant_id),
    original_call_sid TEXT NOT NULL REFERENCES calls(call_sid),
    original_call_id TEXT,
    target_number TEXT NOT NULL,
    requested_by TEXT,
    reason TEXT,
    status TEXT NOT NULL DEFAULT 'requested',
    audit_note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_callback_requests_call
    ON callback_requests(original_call_sid, id DESC);

CREATE INDEX IF NOT EXISTS idx_callback_requests_merchant
    ON callback_requests(merchant_id, id DESC);
