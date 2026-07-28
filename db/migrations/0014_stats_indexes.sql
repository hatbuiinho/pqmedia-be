CREATE INDEX IF NOT EXISTS post_comments_created_at_author_idx
ON post_comments (created_at DESC, author_user_id);

CREATE INDEX IF NOT EXISTS reactions_updated_at_user_idx
ON reactions (updated_at DESC, user_id);
