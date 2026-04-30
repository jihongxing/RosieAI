CREATE TABLE IF NOT EXISTS business_result_retries (
    id BIGSERIAL PRIMARY KEY,
    session_id TEXT NOT NULL UNIQUE,
    call_sid TEXT NOT NULL,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'failed',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_business_result_retries_status ON business_result_retries(status, id);
CREATE INDEX IF NOT EXISTS idx_business_result_retries_call_sid ON business_result_retries(call_sid);
