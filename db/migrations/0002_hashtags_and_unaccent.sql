-- Add unaccent extension and wrapper function
CREATE EXTENSION IF NOT EXISTS unaccent;

CREATE OR REPLACE FUNCTION f_unaccent(text)
  RETURNS text AS
$func$
SELECT public.unaccent('public.unaccent', $1)
$func$  LANGUAGE sql IMMUTABLE PARALLEL SAFE STRICT;

-- Create index on posts content for unaccented search
CREATE INDEX IF NOT EXISTS posts_content_unaccent_idx ON posts (f_unaccent(content));

-- Hashtags table
CREATE TABLE hashtags (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Post Hashtags mapping table
CREATE TABLE post_hashtags (
    post_id    UUID NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    hashtag_id UUID NOT NULL REFERENCES hashtags(id) ON DELETE RESTRICT,
    UNIQUE (post_id, hashtag_id)
);

CREATE INDEX post_hashtags_hashtag_idx ON post_hashtags (hashtag_id);
