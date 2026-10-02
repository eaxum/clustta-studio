package sync_service

import (
	"clustta/internal/compatibility"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"google.golang.org/protobuf/proto"
)

// ProjectDataBytesForAPI projects canonical sync data into the selected wire contract.
func ProjectDataBytesForAPI(data []byte, apiVersion string) ([]byte, error) {
	if apiVersion != compatibility.LegacyAPIVersion {
		return data, nil
	}

	var projectData repositorypb.ProjectData
	if err := proto.Unmarshal(data, &projectData); err != nil {
		return nil, err
	}
	projectData.AssetCheckpointTags = nil
	projectData.Tomb = legacyTombs(projectData.Tomb)
	for _, dependency := range projectData.AssetDependencies {
		dependency.ResolutionMode = ""
		dependency.CheckpointId = ""
		dependency.AssetCheckpointTagId = ""
	}
	for _, checkpoint := range projectData.AssetsCheckpoints {
		checkpoint.SourceCheckpointId = nil
	}
	for _, role := range projectData.Roles {
		clearProjectManagementPermissions(role)
	}
	return proto.Marshal(&projectData)
}

// PreserveCanonicalFieldsForAPI prevents legacy writes from clearing newer fields.
func PreserveCanonicalFieldsForAPI(tx *sqlx.Tx, apiVersion string, data *repositorypb.ProjectData) error {
	if apiVersion != compatibility.LegacyAPIVersion {
		return nil
	}
	for _, dependency := range data.AssetDependencies {
		var resolutionMode string
		var checkpointID sql.NullString
		var checkpointTagID sql.NullString
		err := tx.QueryRowx(
			"SELECT resolution_mode, checkpoint_id, asset_checkpoint_tag_id FROM asset_dependency WHERE id = ?",
			dependency.Id,
		).Scan(&resolutionMode, &checkpointID, &checkpointTagID)
		if err == sql.ErrNoRows {
			dependency.ResolutionMode = repository.DependencyResolutionFloating
			continue
		}
		if err != nil {
			return err
		}
		dependency.ResolutionMode = resolutionMode
		if checkpointID.Valid {
			dependency.CheckpointId = checkpointID.String
		}
		if checkpointTagID.Valid {
			dependency.AssetCheckpointTagId = checkpointTagID.String
		}
	}
	for _, checkpoint := range data.AssetsCheckpoints {
		var sourceCheckpointID sql.NullString
		err := tx.QueryRowx(
			"SELECT source_checkpoint_id FROM asset_checkpoint WHERE id = ?",
			checkpoint.Id,
		).Scan(&sourceCheckpointID)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return err
		}
		if sourceCheckpointID.Valid {
			source := sourceCheckpointID.String
			checkpoint.SourceCheckpointId = &source
		}
	}
	for _, role := range data.Roles {
		err := tx.QueryRowx(
			`SELECT
				manage_collection_types,
				manage_asset_types,
				manage_dependency_types,
				manage_statuses,
				manage_tags,
				manage_workflows,
				manage_integrations,
				manage_project_settings,
				manage_roles
			FROM role WHERE id = ?`,
			role.Id,
		).Scan(
			&role.ManageCollectionTypes,
			&role.ManageAssetTypes,
			&role.ManageDependencyTypes,
			&role.ManageStatuses,
			&role.ManageTags,
			&role.ManageWorkflows,
			&role.ManageIntegrations,
			&role.ManageProjectSettings,
			&role.ManageRoles,
		)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
	}
	data.AssetCheckpointTags = nil
	data.Tomb = legacyTombs(data.Tomb)
	return nil
}

func legacyTombs(tombs []*repositorypb.Tomb) []*repositorypb.Tomb {
	result := make([]*repositorypb.Tomb, 0, len(tombs))
	for _, tomb := range tombs {
		if tomb.TableName != "asset_checkpoint_tag" {
			result = append(result, tomb)
		}
	}
	return result
}

func clearProjectManagementPermissions(role *repositorypb.Role) {
	role.ManageCollectionTypes = false
	role.ManageAssetTypes = false
	role.ManageDependencyTypes = false
	role.ManageStatuses = false
	role.ManageTags = false
	role.ManageWorkflows = false
	role.ManageIntegrations = false
	role.ManageProjectSettings = false
	role.ManageRoles = false
}
