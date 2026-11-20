CREATE TABLE report_feedback (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    rating ENUM('helpful','not_helpful') NOT NULL,
    comment VARCHAR(500) CHARACTER SET utf8mb4 NOT NULL DEFAULT '',
    PRIMARY KEY (tenant_id, task_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_reports(tenant_id, task_id) ON DELETE CASCADE
) ENGINE=InnoDB;
