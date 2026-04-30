CREATE TABLE IF NOT EXISTS merchant_profiles (
    merchant_id TEXT PRIMARY KEY REFERENCES merchants(merchant_id),
    industry TEXT NOT NULL DEFAULT 'hair_salon',
    address TEXT,
    business_hours TEXT,
    services TEXT[] NOT NULL DEFAULT '{}',
    faq_items JSONB NOT NULL DEFAULT '[]'::jsonb,
    appointment_rules TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
