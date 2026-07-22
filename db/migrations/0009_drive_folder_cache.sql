ALTER TABLE posts
ADD COLUMN drive_target_folder_id text;

CREATE TABLE google_drive_folders (
	root_folder_id text NOT NULL,
	folder_id text NOT NULL,
	parent_folder_id text,
	name text NOT NULL,
	path text NOT NULL,
	depth integer NOT NULL DEFAULT 0,
	sort_order integer NOT NULL DEFAULT 0,
	last_synced_at timestamptz NOT NULL DEFAULT now(),
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	PRIMARY KEY (root_folder_id, folder_id)
);

CREATE INDEX google_drive_folders_root_path_idx
	ON google_drive_folders (root_folder_id, path, sort_order, folder_id);
