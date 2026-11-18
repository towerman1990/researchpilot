CREATE TABLE model_configs (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    version INT UNSIGNED NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    model VARCHAR(128) NOT NULL,
    key_cipher BLOB NOT NULL,
    key_tail VARCHAR(4) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(tenant_id,version),
    FOREIGN KEY(tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;
ALTER TABLE research_tasks ADD model_version INT UNSIGNED NULL;
CREATE TABLE research_sources (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    source_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    title VARCHAR(512) NOT NULL,
    canonical_url VARCHAR(2048) NOT NULL,
    excerpt MEDIUMTEXT NOT NULL,
    fingerprint BINARY(32) NOT NULL,
    retrieved_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(tenant_id,task_id,source_id),
    UNIQUE KEY uq_evidence(tenant_id,task_id,fingerprint),
    FOREIGN KEY(tenant_id,task_id) REFERENCES research_tasks(tenant_id,id)
) ENGINE=InnoDB;
ALTER TABLE research_reports ADD kind VARCHAR(16) NOT NULL DEFAULT 'demo';
CREATE TABLE report_citations (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ordinal INT UNSIGNED NOT NULL,
    source_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    PRIMARY KEY(tenant_id,task_id,ordinal),
    FOREIGN KEY(tenant_id,task_id) REFERENCES research_reports(tenant_id,task_id),
    FOREIGN KEY(tenant_id,task_id,source_id) REFERENCES research_sources(tenant_id,task_id,source_id)
) ENGINE=InnoDB;
