package migrations

import (
	"clustta/internal/utils"

	"github.com/jmoiron/sqlx"
)

var projectManagementPermissions = []string{
	"manage_collection_types",
	"manage_asset_types",
	"manage_dependency_types",
	"manage_statuses",
	"manage_tags",
	"manage_workflows",
	"manage_integrations",
	"manage_project_settings",
	"manage_roles",
}

// MigrateV2_2 adds versioned dependencies and project management permissions.
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
	if err := utils.AddColumnIfNotExist(db, "asset_checkpoint", "source_checkpoint_id", "TEXT", "", true); err != nil {
		return err
	}
	return utils.CreateSchema(db, schema)
}

func prepareProjectManagementPermissions(db *sqlx.DB) error {
	for _, permission := range projectManagementPermissions {
		if err := utils.AddColumnIfNotExist(db, "role", permission, "BOOLEAN", "0", false); err != nil {
			return err
		}
	}

	_, err := db.Exec(`
		UPDATE role SET
			manage_collection_types = 1,
			manage_asset_types = 1,
			manage_dependency_types = 1,
			manage_statuses = 1,
			manage_tags = 1,
			manage_workflows = 1,
			manage_integrations = 1,
			manage_project_settings = 1,
			manage_roles = 1
		WHERE lower(name) = 'admin'
	`)
	return err
}

func prepareDependencyColumns(db *sqlx.DB) error {
	for _, table := range []string{"task_checkpoint", "asset_checkpoint"} {
		exists, err := utils.TableExists(db, table)
		if err != nil {
			return err
		}
		if exists {
			if err = utils.AddColumnIfNotExist(db, table, "source_checkpoint_id", "TEXT", "", true); err != nil {
				return err
			}
		}
	}
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
