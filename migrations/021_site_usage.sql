CREATE TABLE site_research_usage (
    task_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
    usage_day DATE NOT NULL,
    subject BINARY(32) NOT NULL,
    KEY ix_site_usage_day (usage_day, subject),
    KEY ix_site_usage_subject (subject)
) ENGINE=InnoDB;
