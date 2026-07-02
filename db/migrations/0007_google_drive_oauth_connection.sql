CREATE TABLE google_drive_oauth_connections (
    provider                TEXT PRIMARY KEY,
    google_email            TEXT NOT NULL,
    encrypted_refresh_token TEXT NOT NULL,
    scope                   TEXT NOT NULL DEFAULT '',
    token_type              TEXT,
    connected_by_user_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    connected_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_refresh_at         TIMESTAMPTZ,
    last_error              TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);
