CREATE INDEX IF NOT EXISTS idx_callback_requests_status
    ON callback_requests(status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_inbox_items_status
    ON inbox_items(merchant_id, status, id DESC);
