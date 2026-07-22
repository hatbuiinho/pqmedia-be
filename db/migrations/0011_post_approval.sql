ALTER TABLE posts
	ADD COLUMN IF NOT EXISTS is_approved BOOLEAN NOT NULL DEFAULT FALSE,
	ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ NULL,
	ADD COLUMN IF NOT EXISTS approved_by_user_id UUID NULL REFERENCES users(id);

CREATE INDEX IF NOT EXISTS idx_posts_is_approved_created_at
	ON posts (is_approved, created_at DESC)
	WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_posts_approved_by_user_id
	ON posts (approved_by_user_id)
	WHERE deleted_at IS NULL;
