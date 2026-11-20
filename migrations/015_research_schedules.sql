CREATE TABLE research_schedules (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    goal VARCHAR(2000) CHARACTER SET utf8mb4 NOT NULL,
    interval_days TINYINT UNSIGNED NOT NULL,
    status ENUM('active','completed','cancelled','blocked') NOT NULL DEFAULT 'active',
    next_run DATETIME(6) NULL,
    last_task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, id),
    INDEX ix_schedule_due (status, next_run, tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;
