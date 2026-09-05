package migrations

import (
	"clustta/internal/utils"

	"github.com/jmoiron/sqlx"
)

// MigrateV2_2 adds checkpoint tags and versioned dependency selectors.
func MigrateV2_2(db *sqlx.DB, schema string) error {
	_, err := db.Exec(`
		DROP VIEW IF EXISTS full_asset;
		DROP VIEW IF EXISTS asset_dependencies;
		DROP TRIGGER IF EXISTS asset_dependency_selector_insert;
		DROP TRIGGER IF EXISTS asset_dependency_selector_update;
		DROP TRIGGER IF EXISTS asset_checkpoint_tag_dependency_delete;
	`)
	if err != nil {
		return err
	}

	if err := utils.AddColumnIfNotExist(db, "asset_dependency", "resolution_mode", "TEXT", "'floating'", false); err != nil {
		return err
	}
	if err := utils.AddColumnIfNotExist(db, "asset_dependency", "checkpoint_id", "TEXT", "", true); err != nil {
		return err
	}
	if err := utils.AddColumnIfNotExist(db, "asset_dependency", "asset_checkpoint_tag_id", "TEXT", "", true); err != nil {
		return err
	}
	return utils.CreateSchema(db, schema)
}

func prepareDependencyColumns(db *sqlx.DB) error {
	for _, table := range []string{"task_dependency", "asset_dependency"} {
		exists, err := utils.TableExists(db, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if err := utils.AddColumnIfNotExist(db, table, "resolution_mode", "TEXT", "'floating'", false); err != nil {
			return err
		}
		for _, column := range []string{"checkpoint_id", "asset_checkpoint_tag_id"} {
			if err := utils.AddColumnIfNotExist(db, table, column, "TEXT", "", true); err != nil {
				return err
			}
		}
	}
	return nil
}
