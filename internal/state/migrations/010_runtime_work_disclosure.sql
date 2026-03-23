-- NTM State Store: Runtime Work Disclosure Fields
-- Version: 010
-- Description: Persists machine-readable disclosure metadata for runtime work titles
-- Bead: bd-j9jo3.3.5

ALTER TABLE runtime_work ADD COLUMN title_disclosure TEXT;
