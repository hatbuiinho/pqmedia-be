ALTER TABLE posts
	ADD COLUMN IF NOT EXISTS approval_status TEXT NOT NULL DEFAULT 'pending',
	ADD COLUMN IF NOT EXISTS reviewed_at TIMESTAMPTZ NULL,
	ADD COLUMN IF NOT EXISTS reviewed_by_user_id UUID NULL REFERENCES users(id);

DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_constraint
		WHERE conname = 'posts_approval_status_check'
	) THEN
		ALTER TABLE posts
			ADD CONSTRAINT posts_approval_status_check
			CHECK (approval_status IN ('pending', 'approved', 'rejected'));
	END IF;
END $$;

UPDATE posts
SET
	approval_status = CASE
		WHEN is_approved THEN 'approved'
		ELSE 'pending'
	END,
	reviewed_at = COALESCE(reviewed_at, approved_at),
	reviewed_by_user_id = COALESCE(reviewed_by_user_id, approved_by_user_id)
WHERE is_approved = TRUE
   OR approved_at IS NOT NULL
   OR approved_by_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_posts_approval_status_created_at
	ON posts (approval_status, created_at DESC)
	WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_posts_reviewed_by_user_id
	ON posts (reviewed_by_user_id)
	WHERE deleted_at IS NULL;
