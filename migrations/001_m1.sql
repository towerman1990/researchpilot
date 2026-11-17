CREATE TABLE users (
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    email VARCHAR(254) NOT NULL UNIQUE,
    password_hash VARBINARY(100) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE sessions (
    token_hash BINARY(32) PRIMARY KEY,
    user_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX ix_sessions_expiry (expires_at),
    FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE research_tasks (
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    goal VARCHAR(2000) NOT NULL,
    status ENUM('QUEUED','RUNNING','SUCCEEDED','FAILED') NOT NULL,
    phase ENUM('planning','synthesizing') NULL,
    generation INT UNSIGNED NOT NULL DEFAULT 1,
    lease_epoch INT UNSIGNED NOT NULL DEFAULT 0,
    lease_until DATETIME(6) NULL,
    deadline_at DATETIME(6) NOT NULL,
    event_seq BIGINT UNSIGNED NOT NULL,
    error_code VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_task_tenant (tenant_id, id),
    INDEX ix_task_history (tenant_id, created_at, id),
    INDEX ix_task_lease (status, lease_until),
    INDEX ix_task_deadline (status, deadline_at),
    FOREIGN KEY (tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE request_keys (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    key_digest BINARY(32) NOT NULL,
    request_digest BINARY(32) NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY (tenant_id, key_digest),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id)
) ENGINE=InnoDB;

CREATE TABLE task_events (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    seq BIGINT UNSIGNED NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    payload JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (tenant_id, task_id, seq),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id)
) ENGINE=InnoDB;

CREATE TABLE task_outbox (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    generation INT UNSIGNED NOT NULL,
    available_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    claim_epoch INT UNSIGNED NOT NULL DEFAULT 0,
    claim_until DATETIME(6) NULL,
    sent_at DATETIME(6) NULL,
    PRIMARY KEY (tenant_id, task_id, generation),
    INDEX ix_outbox_relay (sent_at, available_at, claim_until),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id)
) ENGINE=InnoDB;

CREATE TABLE research_reports (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    markdown TEXT NOT NULL,
    PRIMARY KEY (tenant_id, task_id),
    FOREIGN KEY (tenant_id, task_id) REFERENCES research_tasks(tenant_id, id)
) ENGINE=InnoDB;
