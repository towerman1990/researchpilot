ALTER TABLE model_configs ADD is_trial BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE trial_usage (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, task_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id)
) ENGINE=InnoDB;
