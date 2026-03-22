ALTER TABLE attention_events ADD COLUMN reason_code TEXT;

CREATE INDEX IF NOT EXISTS idx_attention_events_reason_code ON attention_events(reason_code);
