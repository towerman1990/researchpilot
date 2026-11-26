CREATE TABLE knowledge_libraries (
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(120) NOT NULL,
    UNIQUE KEY uq_knowledge_library_owner (tenant_id, id),
    FOREIGN KEY (tenant_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE knowledge_versions (
    id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    library_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    name VARCHAR(240) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL,
    revision INT UNSIGNED NOT NULL,
    content TEXT NOT NULL,
    status ENUM('QUEUED','READY') NOT NULL DEFAULT 'QUEUED',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_knowledge_document_revision (tenant_id, library_id, name, revision),
    UNIQUE KEY uq_knowledge_version_owner (tenant_id, id),
    KEY ix_knowledge_queue (status, created_at, id),
    FOREIGN KEY (tenant_id, library_id) REFERENCES knowledge_libraries(tenant_id, id)
) ENGINE=InnoDB;

CREATE TABLE knowledge_chunks (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    version_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    ordinal INT UNSIGNED NOT NULL,
    excerpt TEXT NOT NULL,
    PRIMARY KEY (tenant_id, version_id, ordinal),
    FOREIGN KEY (tenant_id, version_id) REFERENCES knowledge_versions(tenant_id, id)
) ENGINE=InnoDB;
