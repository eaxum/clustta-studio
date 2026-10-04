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
func MigrateV2_2(tx *sqlx.Tx, _ string) error {
	_, err := tx.Exec(`
		DROP VIEW IF EXISTS full_asset;
		DROP VIEW IF EXISTS asset_dependencies;
		DROP TRIGGER IF EXISTS asset_dependency_selector_insert;
		DROP TRIGGER IF EXISTS asset_dependency_selector_update;
		DROP TRIGGER IF EXISTS asset_checkpoint_tag_dependency_delete;
	`)
	if err != nil {
		return err
	}

	if err := prepareV2_2Columns(tx); err != nil {
		return err
	}
	return prepareProjectManagementPermissions(tx)
}

func prepareProjectManagementPermissions(tx *sqlx.Tx) error {
	for _, permission := range projectManagementPermissions {
		if err := utils.AddColumnIfNotExist(tx, "role", permission, "BOOLEAN", "0", false); err != nil {
			return err
		}
	}

	_, err := tx.Exec(`
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

func repairV2_2Schema(tx *sqlx.Tx) error {
	if err := prepareProjectManagementPermissions(tx); err != nil {
		return err
	}
	return prepareV2_2Columns(tx)
}

func prepareV2_2Columns(tx *sqlx.Tx) error {
	checkpointExists, err := utils.TableExists(tx, "asset_checkpoint")
	if err != nil {
		return err
	}
	if checkpointExists {
		if err = utils.AddColumnIfNotExist(tx, "asset_checkpoint", "source_checkpoint_id", "TEXT", "", true); err != nil {
			return err
		}
	}

	dependencyExists, err := utils.TableExists(tx, "asset_dependency")
	if err != nil {
		return err
	}
	if !dependencyExists {
		return nil
	}
	if err = utils.AddColumnIfNotExist(tx, "asset_dependency", "resolution_mode", "TEXT", "'floating'", false); err != nil {
		return err
	}
	if err = utils.AddColumnIfNotExist(tx, "asset_dependency", "checkpoint_id", "TEXT", "", true); err != nil {
		return err
	}
	return utils.AddColumnIfNotExist(tx, "asset_dependency", "asset_checkpoint_tag_id", "TEXT", "", true)
}
