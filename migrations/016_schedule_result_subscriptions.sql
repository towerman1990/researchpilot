ALTER TABLE research_schedules ADD COLUMN notify_results BOOLEAN NOT NULL DEFAULT false;

CREATE TABLE research_schedule_results (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    schedule_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    read_at DATETIME(6) NULL,
    PRIMARY KEY (tenant_id, task_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id),
    FOREIGN KEY (tenant_id, schedule_id) REFERENCES research_schedules(tenant_id, id)
) ENGINE=InnoDB;
