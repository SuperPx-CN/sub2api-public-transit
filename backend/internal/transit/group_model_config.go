package transit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type groupModelConfigColumn string

const (
	groupModelConfigNone           groupModelConfigColumn = ""
	groupModelConfigLegacy         groupModelConfigColumn = "models_list_config"
	groupModelConfigAllowlist      groupModelConfigColumn = "model_allowlist"
	missingGroupModelConfigWarning                        = "Group model configuration is unavailable (model_allowlist and models_list_config are absent); only the channel pricing catalog is included."
)

// Resolve through the same search_path as the data queries. pg_attribute does
// not hide ungranted columns, unlike information_schema.columns. Re-probe in
// every read-only transaction so a running process can follow schema renames.
func detectGroupModelConfig(ctx context.Context, tx *sql.Tx) (groupModelConfigColumn, error) {
	var name string
	var validType bool
	err := tx.QueryRowContext(ctx, `SELECT attname, atttypid IN ('pg_catalog.json'::regtype, 'pg_catalog.jsonb'::regtype)
FROM pg_catalog.pg_attribute
WHERE attrelid = 'groups'::regclass AND attnum > 0 AND NOT attisdropped
  AND attname IN ('model_allowlist', 'models_list_config')
ORDER BY CASE attname WHEN 'model_allowlist' THEN 0 ELSE 1 END
LIMIT 1`).Scan(&name, &validType)
	if errors.Is(err, sql.ErrNoRows) {
		return groupModelConfigNone, nil
	}
	if err != nil {
		return groupModelConfigNone, err
	}
	if !validType {
		return groupModelConfigNone, errors.New("invalid group model configuration column type")
	}
	// Only hardcoded identifiers may enter the SELECT below.
	switch name {
	case string(groupModelConfigAllowlist):
		return groupModelConfigAllowlist, nil
	case string(groupModelConfigLegacy):
		return groupModelConfigLegacy, nil
	default:
		return groupModelConfigNone, errors.New("unexpected group model configuration column")
	}
}

// Shared by preflight and snapshot reads. Reading only id + the selected JSON
// column keeps column-level grants sufficient and validates public rows even
// when a snapshot is cached. SQL NULL and JSON null mean disabled configuration.
func readGroupModelConfigs(ctx context.Context, tx *sql.Tx, column groupModelConfigColumn, visit func(int64, GroupModelsListConfig)) error {
	var projection string
	switch column {
	case groupModelConfigNone:
		return nil
	case groupModelConfigAllowlist:
		projection = "model_allowlist"
	case groupModelConfigLegacy:
		projection = "models_list_config"
	default:
		return errors.New("unexpected group model configuration column")
	}
	return readRows(ctx, tx, "SELECT id,"+projection+" FROM groups WHERE status='active' AND NOT is_exclusive AND deleted_at IS NULL ORDER BY id", nil, func(r *sql.Rows) error {
		var id int64
		var raw []byte
		if err := r.Scan(&id, &raw); err != nil {
			return err
		}
		var cfg GroupModelsListConfig
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &cfg); err != nil {
				return err
			}
		}
		if visit != nil {
			visit(id, cfg)
		}
		return nil
	})
}
