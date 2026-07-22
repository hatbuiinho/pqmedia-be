ALTER TABLE user_profiles
    ADD COLUMN dharma_name VARCHAR(80),
    ADD COLUMN birth_year SMALLINT,
    ADD COLUMN ctn VARCHAR(40);

ALTER TABLE user_profiles
    ADD CONSTRAINT user_profiles_birth_year_check
    CHECK (birth_year IS NULL OR (birth_year >= 1900 AND birth_year <= 2100));
