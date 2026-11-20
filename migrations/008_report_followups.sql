ALTER TABLE research_tasks
    ADD COLUMN parent_task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    ADD CONSTRAINT fk_research_task_parent FOREIGN KEY (tenant_id, parent_task_id) REFERENCES research_tasks (tenant_id, id);
