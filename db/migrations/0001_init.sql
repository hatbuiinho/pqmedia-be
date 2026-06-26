-- PQ Media canonical schema v1.
-- Convention: UUID primary keys via gen_random_uuid(), TIMESTAMPTZ with default now(),
-- soft-delete on posts via deleted_at.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- =========================================================
-- Users + profiles
-- =========================================================
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    is_admin      BOOLEAN NOT NULL DEFAULT FALSE,
    is_active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE user_profiles (
    user_id            UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    full_name          TEXT NOT NULL DEFAULT '',
    phone              TEXT,
    avatar_bucket      TEXT,
    avatar_object_key  TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX user_profiles_phone_uniq ON user_profiles (phone) WHERE phone IS NOT NULL;

-- =========================================================
-- Posts
-- =========================================================
CREATE TABLE posts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    author_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    content         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX posts_author_idx ON posts (author_user_id);
CREATE INDEX posts_created_at_idx ON posts (created_at DESC) WHERE deleted_at IS NULL;

CREATE TYPE attachment_kind AS ENUM ('image', 'video');

CREATE TABLE post_attachments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id       UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    kind          attachment_kind NOT NULL,
    file_name     TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    bucket        TEXT NOT NULL,
    object_key    TEXT NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    width         INT,
    height        INT,
    duration_ms   INT,
    sort_order    INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX post_attachments_post_idx ON post_attachments (post_id, sort_order);

-- =========================================================
-- Comments + reactions
-- =========================================================
CREATE TABLE post_comments (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id         UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    author_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX post_comments_post_idx ON post_comments (post_id, created_at ASC);
CREATE INDEX post_comments_author_idx ON post_comments (author_user_id);

CREATE TABLE reactions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    target_type  VARCHAR(32) NOT NULL,    -- 'post' | 'comment'
    target_id    UUID NOT NULL,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    emoji        VARCHAR(32) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (target_type, target_id, user_id)
);

CREATE INDEX reactions_target_idx ON reactions (target_type, target_id);

-- =========================================================
-- Post publications (đã đăng MXH ngoài)
-- =========================================================
CREATE TABLE post_publications (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    post_id               UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    platform              VARCHAR(32) NOT NULL,   -- facebook|instagram|tiktok|threads|youtube|x|other
    external_url          TEXT,
    published_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_by_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    note                  TEXT,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (post_id, platform)
);

CREATE INDEX post_publications_platform_idx ON post_publications (platform);

-- =========================================================
-- Notifications + web push subscriptions
-- =========================================================
CREATE TABLE notifications (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipient_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    kind               VARCHAR(64) NOT NULL,
    post_id            UUID REFERENCES posts(id) ON DELETE CASCADE,
    comment_id         UUID REFERENCES post_comments(id) ON DELETE CASCADE,
    title              TEXT NOT NULL DEFAULT '',
    body               TEXT NOT NULL DEFAULT '',
    route_url          TEXT,
    payload            JSONB NOT NULL DEFAULT '{}'::jsonb,
    read_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX notifications_recipient_created_idx ON notifications (recipient_user_id, created_at DESC);
CREATE INDEX notifications_unread_idx ON notifications (recipient_user_id) WHERE read_at IS NULL;

CREATE TABLE web_push_subscriptions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint      TEXT UNIQUE NOT NULL,
    p256dh        TEXT NOT NULL,
    auth          TEXT NOT NULL,
    user_agent    TEXT,
    device_label  TEXT,
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    disabled_at   TIMESTAMPTZ
);

CREATE INDEX web_push_user_idx ON web_push_subscriptions (user_id);
CREATE INDEX web_push_enabled_idx ON web_push_subscriptions (enabled, updated_at DESC);
