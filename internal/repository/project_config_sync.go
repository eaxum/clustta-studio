package repository

import (
	"clustta/internal/repository/repositorypb"
	"database/sql"
	"fmt"

	"github.com/jmoiron/sqlx"
)

var SyncableProjectConfigNames = []string{
	"project_script_settings_v1",
	"dcc_prelaunch_hooks_v1",
}

func GetSyncableProjectConfigs(tx *sqlx.Tx, changedOnly bool) ([]ProjectConfig, error) {
	query, args, err := sqlx.In("SELECT name, value, mtime, synced FROM config WHERE name IN (?)", SyncableProjectConfigNames)
	if err != nil {
		return nil, err
	}
	if changedOnly {
		query += " AND synced = 0"
	}
	configs := []ProjectConfig{}
	if err := tx.Select(&configs, tx.Rebind(query), args...); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	return configs, nil
}

func ApplySyncableProjectConfigs(tx *sqlx.Tx, configs []ProjectConfig) error {
	allowed := map[string]bool{}
	for _, name := range SyncableProjectConfigNames {
		allowed[name] = true
	}
	for _, config := range configs {
		if !allowed[config.Name] {
			return fmt.Errorf("project config %q is not syncable", config.Name)
		}
		if _, err := tx.Exec(`
			INSERT INTO config (name, value, mtime, synced)
			VALUES (?, ?, ?, 1)
			ON CONFLICT (name) DO UPDATE SET
				value = EXCLUDED.value,
				mtime = EXCLUDED.mtime,
				synced = 1
			WHERE EXCLUDED.mtime >= config.mtime
		`, config.Name, config.Value, config.Mtime); err != nil {
			return err
		}
	}
	return nil
}

func MarkSyncableProjectConfigsSynced(tx *sqlx.Tx) error {
	query, args, err := sqlx.In("UPDATE config SET synced = 1 WHERE name IN (?)", SyncableProjectConfigNames)
	if err != nil {
		return err
	}
	_, err = tx.Exec(tx.Rebind(query), args...)
	return err
}

func ToPbProjectConfigs(configs []ProjectConfig) []*repositorypb.ProjectConfig {
	result := make([]*repositorypb.ProjectConfig, 0, len(configs))
	for _, config := range configs {
		result = append(result, &repositorypb.ProjectConfig{
			Name: config.Name, Value: config.Value, Mtime: int64(config.Mtime),
		})
	}
	return result
}

func FromPbProjectConfigs(configs []*repositorypb.ProjectConfig) []ProjectConfig {
	result := make([]ProjectConfig, 0, len(configs))
	for _, config := range configs {
		result = append(result, ProjectConfig{
			Name: config.Name, Value: config.Value, Mtime: int(config.Mtime), Synced: true,
		})
	}
	return result
}
