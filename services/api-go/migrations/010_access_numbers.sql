-- Platform-owned Rosie access number pool.
-- Merchants keep access_number as the fast routing field, while this table owns
-- allocation lifecycle, provider metadata and recycling state.

ALTER TABLE merchants
    ALTER COLUMN access_number DROP NOT NULL;

CREATE TABLE IF NOT EXISTS access_numbers (
    id BIGSERIAL PRIMARY KEY,
    number TEXT NOT NULL UNIQUE,
    provider TEXT,
    provider_number_id TEXT,
    trunk_id TEXT,
    jambonz_application_id TEXT,
    status TEXT NOT NULL DEFAULT 'available',
    merchant_id TEXT REFERENCES merchants(merchant_id),
    notes TEXT,
    assigned_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT access_numbers_status_check
        CHECK (status IN ('available', 'reserved', 'assigned', 'disabled'))
);

CREATE INDEX IF NOT EXISTS idx_access_numbers_status ON access_numbers(status, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_access_numbers_assigned_merchant
    ON access_numbers(merchant_id)
    WHERE status = 'assigned' AND merchant_id IS NOT NULL;

INSERT INTO access_numbers (number, status, merchant_id, assigned_at, notes)
SELECT access_number, 'assigned', merchant_id, now(), 'backfilled from merchants.access_number'
FROM merchants
WHERE COALESCE(access_number, '') <> ''
ON CONFLICT (number) DO NOTHING;
