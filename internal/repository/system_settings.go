package repository

import (
	"context"
	"fmt"
)

type SystemSetting struct {
	Key   string
	Value string
}

func (r *Repo) ListSystemSettingsByKeys(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT key, value
		FROM system_settings
		WHERE key = ANY($1)
	`, keys)
	if err != nil {
		return nil, fmt.Errorf("list system settings: %w", err)
	}
	defer rows.Close()

	out := make(map[string]string, len(keys))
	for rows.Next() {
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan system setting: %w", err)
		}
		out[key] = value
	}
	return out, rows.Err()
}

func (r *Repo) UpsertSystemSetting(ctx context.Context, key, value string) error {
	if _, err := r.pool.Exec(ctx, `
		INSERT INTO system_settings (key, value, updated_at)
		VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value,
		    updated_at = now()
	`, key, value); err != nil {
		return fmt.Errorf("upsert system setting %s: %w", key, err)
	}
	return nil
}
