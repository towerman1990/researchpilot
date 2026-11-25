CREATE TABLE guest_trial_usage (
    trial_day DATE NOT NULL,
    subject BINARY(32) NOT NULL,
    used TINYINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (trial_day, subject)
) ENGINE=InnoDB;
