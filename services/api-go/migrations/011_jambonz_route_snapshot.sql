-- Snapshot of jambonz-side routing metadata imported from config export or API.

ALTER TABLE access_numbers
    ADD COLUMN IF NOT EXISTS jambonz_application_name TEXT,
    ADD COLUMN IF NOT EXISTS jambonz_call_hook_url TEXT,
    ADD COLUMN IF NOT EXISTS jambonz_status_hook_url TEXT,
    ADD COLUMN IF NOT EXISTS jambonz_config_synced_at TIMESTAMPTZ;
