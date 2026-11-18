ALTER TABLE research_tasks
    MODIFY status ENUM('QUEUED','RUNNING','SUCCEEDED','FAILED','RETRY_WAIT','CANCELLING','CANCELLED','PARTIAL') NOT NULL,
    MODIFY phase ENUM('planning','synthesizing','executing','observing','replanning') NULL,
    ADD runtime_version VARCHAR(16) NOT NULL DEFAULT 'm1',
    ADD retry_count SMALLINT UNSIGNED NOT NULL DEFAULT 0,
    ADD retry_at DATETIME(6) NULL,
    ADD token_limit INT UNSIGNED NOT NULL DEFAULT 40000,
    ADD tokens_reserved INT UNSIGNED NOT NULL DEFAULT 0,
    ADD tokens_used INT UNSIGNED NOT NULL DEFAULT 0,
    ADD cancel_requested_at DATETIME(6) NULL,
    ADD finished_at DATETIME(6) NULL,
    ADD INDEX ix_task_retry(status,retry_at);

CREATE TABLE task_checkpoints (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    step_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    result_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(tenant_id,task_id,step_key),
    FOREIGN KEY(tenant_id,task_id) REFERENCES research_tasks(tenant_id,id)
) ENGINE=InnoDB;

CREATE TABLE execution_attempts (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    step_key VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt_no INT UNSIGNED NOT NULL,
    lease_epoch INT UNSIGNED NOT NULL,
    status ENUM('STARTED','SUCCEEDED','FAILED','ABANDONED') NOT NULL,
    error_code VARCHAR(64) NULL,
    started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    PRIMARY KEY(tenant_id,task_id,step_key,attempt_no),
    FOREIGN KEY(tenant_id,task_id) REFERENCES research_tasks(tenant_id,id)
) ENGINE=InnoDB;

CREATE TABLE model_attempts (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    lease_epoch INT UNSIGNED NOT NULL,
    status ENUM('RESERVED','ACTUAL','ESTIMATED','RELEASED') NOT NULL,
    reserved_tokens INT UNSIGNED NOT NULL,
    charged_tokens INT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    settled_at DATETIME(6) NULL,
    PRIMARY KEY(tenant_id,task_id,attempt_id),
    FOREIGN KEY(tenant_id,task_id) REFERENCES research_tasks(tenant_id,id)
) ENGINE=InnoDB;
