ALTER TABLE notification_logs
    ADD COLUMN IF NOT EXISTS max_attempts INTEGER NOT NULL DEFAULT 5,
    ADD COLUMN IF NOT EXISTS next_retry_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS error_category TEXT;

CREATE INDEX IF NOT EXISTS idx_notification_logs_dispatch_due
    ON notification_logs(status, next_retry_at, id);
