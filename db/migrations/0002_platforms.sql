CREATE TABLE platforms (
    key         VARCHAR(64) PRIMARY KEY,
    label       TEXT NOT NULL,
    icon        TEXT NOT NULL,
    tone        TEXT NOT NULL,
    sort_order  INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO platforms (key, label, icon, tone, sort_order, is_active)
VALUES
    ('facebook', 'Facebook', 'icon-[lucide--facebook]', 'bg-sky-100 text-sky-700', 10, TRUE),
    ('instagram', 'Instagram', 'icon-[lucide--instagram]', 'bg-pink-100 text-pink-700', 20, TRUE),
    ('tiktok', 'TikTok', 'icon-[lucide--music-2]', 'bg-slate-200 text-slate-800', 30, TRUE),
    ('threads', 'Threads', 'icon-[lucide--at-sign]', 'bg-zinc-200 text-zinc-800', 40, TRUE),
    ('youtube', 'YouTube', 'icon-[lucide--youtube]', 'bg-rose-100 text-rose-700', 50, TRUE),
    ('x', 'X', 'icon-[lucide--twitter]', 'bg-slate-900 text-white', 60, TRUE),
    ('other', 'Khác', 'icon-[lucide--ellipsis]', 'bg-slate-100 text-slate-700', 70, TRUE)
ON CONFLICT (key) DO NOTHING;

ALTER TABLE post_publications
    ADD CONSTRAINT post_publications_platform_fk
    FOREIGN KEY (platform) REFERENCES platforms(key)
    ON UPDATE CASCADE
    ON DELETE RESTRICT;
