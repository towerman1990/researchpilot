ALTER TABLE model_attempts
    ADD COLUMN input_tokens INT UNSIGNED NULL,
    ADD COLUMN output_tokens INT UNSIGNED NULL;
