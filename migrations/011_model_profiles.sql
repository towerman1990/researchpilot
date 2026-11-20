CREATE TABLE model_profiles (
    tenant_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    id INT UNSIGNED NOT NULL,
    version INT UNSIGNED NOT NULL,
    label VARCHAR(80) NOT NULL DEFAULT '',
    is_active BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (tenant_id, id),
    UNIQUE KEY uq_model_profile_version (tenant_id, version),
    FOREIGN KEY (tenant_id, version) REFERENCES model_configs(tenant_id, version)
) ENGINE=InnoDB;

INSERT INTO model_profiles (tenant_id,id,version,is_active)
SELECT tenant_id,MAX(version),MAX(version),true
FROM model_configs
WHERE is_trial=false AND revoked=false
GROUP BY tenant_id;
