CREATE TYPE attachment_drive_sync_status AS ENUM ('pending', 'uploading', 'uploaded', 'failed');

CREATE TABLE attachment_drive_uploads (
    attachment_id      UUID PRIMARY KEY REFERENCES post_attachments(id) ON DELETE CASCADE,
    status             attachment_drive_sync_status NOT NULL DEFAULT 'pending',
    drive_file_id      TEXT,
    drive_folder_id    TEXT,
    web_view_link      TEXT,
    web_content_link   TEXT,
    error_message      TEXT,
    attempt_count      INT NOT NULL DEFAULT 0,
    next_attempt_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at    TIMESTAMPTZ,
    uploaded_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX attachment_drive_uploads_status_next_attempt_idx
    ON attachment_drive_uploads (status, next_attempt_at);
