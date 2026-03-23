-- NTM State Store: Runtime coordination disclosure metadata
-- Version: 009
-- Description: Persists sanitized mail subject/preview fields for normalized coordination rows
-- Bead: bd-j9jo3.3.5

ALTER TABLE runtime_coordination ADD COLUMN last_message_subject TEXT;
ALTER TABLE runtime_coordination ADD COLUMN last_message_subject_disclosure TEXT;
ALTER TABLE runtime_coordination ADD COLUMN last_message_preview TEXT;
ALTER TABLE runtime_coordination ADD COLUMN last_message_preview_disclosure TEXT;
