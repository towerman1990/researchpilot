ALTER TABLE model_configs ADD revoked BOOLEAN NOT NULL DEFAULT false, ADD key_version INT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE research_tasks ADD min_event_seq BIGINT UNSIGNED NOT NULL DEFAULT 1;
CREATE INDEX ix_event_retention ON research_tasks(status,finished_at);
