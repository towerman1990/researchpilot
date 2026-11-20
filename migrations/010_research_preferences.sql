CREATE TABLE research_preferences (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    preferences JSON NOT NULL,
    FOREIGN KEY (tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;

ALTER TABLE research_tasks ADD preferences_snapshot JSON NULL;
