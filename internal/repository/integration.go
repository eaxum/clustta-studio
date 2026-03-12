package repository

import (
	"clustta/internal/base_service"
	"clustta/internal/repository/models"
	"clustta/internal/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// CreateIntegrationProject creates a new integration project link.
func CreateIntegrationProject(tx *sqlx.Tx, integrationId, externalProjectId, externalProjectName, apiUrl, syncOptions, linkedByUserId, linkedAt string, enabled bool) (models.IntegrationProject, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_project (id, mtime, integration_id, external_project_id, external_project_name, api_url, sync_options, linked_by_user_id, linked_at, enabled, synced)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0)`
	_, err := tx.Exec(query, id, mtime, integrationId, externalProjectId, externalProjectName, apiUrl, syncOptions, linkedByUserId, linkedAt, enabled)
	if err != nil {
		return models.IntegrationProject{}, err
	}

	return GetIntegrationProject(tx, id)
}

// GetIntegrationProject retrieves an integration project by ID.
func GetIntegrationProject(tx *sqlx.Tx, id string) (models.IntegrationProject, error) {
	var project models.IntegrationProject
	err := tx.Get(&project, "SELECT * FROM integration_project WHERE id = $1", id)
	return project, err
}

// GetIntegrationProjectByIntegrationId retrieves an integration project by integration ID.
func GetIntegrationProjectByIntegrationId(tx *sqlx.Tx, integrationId string) (models.IntegrationProject, error) {
	var project models.IntegrationProject
	err := tx.Get(&project, "SELECT * FROM integration_project WHERE integration_id = $1", integrationId)
	return project, err
}

// GetIntegrationProjects retrieves all integration projects.
func GetIntegrationProjects(tx *sqlx.Tx) ([]models.IntegrationProject, error) {
	var projects []models.IntegrationProject
	err := base_service.GetAll(tx, "integration_project", &projects)
	return projects, err
}

// UpdateIntegrationProject updates an existing integration project.
func UpdateIntegrationProject(tx *sqlx.Tx, id, externalProjectId, externalProjectName, apiUrl, syncOptions string, enabled bool) (models.IntegrationProject, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_project SET mtime = $1, external_project_id = $2, external_project_name = $3, api_url = $4, sync_options = $5, enabled = $6 WHERE id = $7`
	_, err := tx.Exec(query, mtime, externalProjectId, externalProjectName, apiUrl, syncOptions, enabled, id)
	if err != nil {
		return models.IntegrationProject{}, err
	}

	return GetIntegrationProject(tx, id)
}

// DeleteIntegrationProject deletes an integration project by ID.
func DeleteIntegrationProject(tx *sqlx.Tx, id string) error {
	_, err := tx.Exec("DELETE FROM integration_project WHERE id = $1", id)
	return err
}

// CreateCollectionMapping creates a new collection to external entity mapping.
func CreateCollectionMapping(tx *sqlx.Tx, integrationId, externalId, externalType, externalName, externalParentId, externalPath, externalMetadata, collectionId, syncedAt string) (models.IntegrationCollectionMapping, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_collection_mapping (id, mtime, integration_id, external_id, external_type, external_name, external_parent_id, external_path, external_metadata, collection_id, synced_at, synced)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, 0)`
	_, err := tx.Exec(query, id, mtime, integrationId, externalId, externalType, externalName, externalParentId, externalPath, externalMetadata, collectionId, syncedAt)
	if err != nil {
		return models.IntegrationCollectionMapping{}, err
	}

	return GetCollectionMapping(tx, id)
}

// GetCollectionMapping retrieves a collection mapping by ID.
func GetCollectionMapping(tx *sqlx.Tx, id string) (models.IntegrationCollectionMapping, error) {
	var mapping models.IntegrationCollectionMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_collection_mapping WHERE id = $1", id)
	return mapping, err
}

// GetCollectionMappingByExternalId retrieves a collection mapping by external ID and integration ID.
func GetCollectionMappingByExternalId(tx *sqlx.Tx, integrationId, externalId string) (models.IntegrationCollectionMapping, error) {
	var mapping models.IntegrationCollectionMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_collection_mapping WHERE integration_id = $1 AND external_id = $2", integrationId, externalId)
	return mapping, err
}

// GetCollectionMappingByCollectionId retrieves a collection mapping by collection ID and integration ID.
func GetCollectionMappingByCollectionId(tx *sqlx.Tx, integrationId, collectionId string) (models.IntegrationCollectionMapping, error) {
	var mapping models.IntegrationCollectionMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_collection_mapping WHERE integration_id = $1 AND collection_id = $2", integrationId, collectionId)
	return mapping, err
}

// GetCollectionMappings retrieves all collection mappings for an integration.
func GetCollectionMappings(tx *sqlx.Tx, integrationId string) ([]models.IntegrationCollectionMapping, error) {
	var mappings []models.IntegrationCollectionMapping
	err := tx.Select(&mappings, "SELECT * FROM integration_collection_mapping WHERE integration_id = $1", integrationId)
	return mappings, err
}

// GetAllCollectionMappings retrieves all collection mappings.
func GetAllCollectionMappings(tx *sqlx.Tx) ([]models.IntegrationCollectionMapping, error) {
	var mappings []models.IntegrationCollectionMapping
	err := base_service.GetAll(tx, "integration_collection_mapping", &mappings)
	return mappings, err
}

// UpdateCollectionMapping updates an existing collection mapping.
func UpdateCollectionMapping(tx *sqlx.Tx, id, externalType, externalName, externalParentId, externalPath, externalMetadata, collectionId, syncedAt string) (models.IntegrationCollectionMapping, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_collection_mapping SET mtime = $1, external_type = $2, external_name = $3, external_parent_id = $4, external_path = $5, external_metadata = $6, collection_id = $7, synced_at = $8 WHERE id = $9`
	_, err := tx.Exec(query, mtime, externalType, externalName, externalParentId, externalPath, externalMetadata, collectionId, syncedAt, id)
	if err != nil {
		return models.IntegrationCollectionMapping{}, err
	}

	return GetCollectionMapping(tx, id)
}

// DeleteCollectionMapping deletes a collection mapping by ID.
func DeleteCollectionMapping(tx *sqlx.Tx, id string) error {
	_, err := tx.Exec("DELETE FROM integration_collection_mapping WHERE id = $1", id)
	return err
}

// CreateAssetMapping creates a new asset to external task mapping.
func CreateAssetMapping(tx *sqlx.Tx, integrationId, externalId, externalName, externalParentId, externalType, externalStatus, externalAssignees, externalMetadata, assetId, lastPushedCheckpointId, syncedAt string) (models.IntegrationAssetMapping, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_asset_mapping (id, mtime, integration_id, external_id, external_name, external_parent_id, external_type, external_status, external_assignees, external_metadata, asset_id, last_pushed_checkpoint_id, synced_at, synced)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 0)`
	_, err := tx.Exec(query, id, mtime, integrationId, externalId, externalName, externalParentId, externalType, externalStatus, externalAssignees, externalMetadata, assetId, lastPushedCheckpointId, syncedAt)
	if err != nil {
		return models.IntegrationAssetMapping{}, err
	}

	return GetAssetMapping(tx, id)
}

// GetAssetMapping retrieves an asset mapping by ID.
func GetAssetMapping(tx *sqlx.Tx, id string) (models.IntegrationAssetMapping, error) {
	var mapping models.IntegrationAssetMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_asset_mapping WHERE id = $1", id)
	return mapping, err
}

// GetAssetMappingByExternalId retrieves an asset mapping by external ID and integration ID.
func GetAssetMappingByExternalId(tx *sqlx.Tx, integrationId, externalId string) (models.IntegrationAssetMapping, error) {
	var mapping models.IntegrationAssetMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_asset_mapping WHERE integration_id = $1 AND external_id = $2", integrationId, externalId)
	return mapping, err
}

// GetAssetMappingByAssetId retrieves an asset mapping by asset ID and integration ID.
func GetAssetMappingByAssetId(tx *sqlx.Tx, integrationId, assetId string) (models.IntegrationAssetMapping, error) {
	var mapping models.IntegrationAssetMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_asset_mapping WHERE integration_id = $1 AND asset_id = $2", integrationId, assetId)
	return mapping, err
}

// GetAssetMappings retrieves all asset mappings for an integration.
func GetAssetMappings(tx *sqlx.Tx, integrationId string) ([]models.IntegrationAssetMapping, error) {
	var mappings []models.IntegrationAssetMapping
	err := tx.Select(&mappings, "SELECT * FROM integration_asset_mapping WHERE integration_id = $1", integrationId)
	return mappings, err
}

// GetAllAssetMappings retrieves all asset mappings.
func GetAllAssetMappings(tx *sqlx.Tx) ([]models.IntegrationAssetMapping, error) {
	var mappings []models.IntegrationAssetMapping
	err := base_service.GetAll(tx, "integration_asset_mapping", &mappings)
	return mappings, err
}

// UpdateAssetMapping updates an existing asset mapping.
func UpdateAssetMapping(tx *sqlx.Tx, id, externalName, externalParentId, externalType, externalStatus, externalAssignees, externalMetadata, assetId, lastPushedCheckpointId, syncedAt string) (models.IntegrationAssetMapping, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_asset_mapping SET mtime = $1, external_name = $2, external_parent_id = $3, external_type = $4, external_status = $5, external_assignees = $6, external_metadata = $7, asset_id = $8, last_pushed_checkpoint_id = $9, synced_at = $10 WHERE id = $11`
	_, err := tx.Exec(query, mtime, externalName, externalParentId, externalType, externalStatus, externalAssignees, externalMetadata, assetId, lastPushedCheckpointId, syncedAt, id)
	if err != nil {
		return models.IntegrationAssetMapping{}, err
	}

	return GetAssetMapping(tx, id)
}

// DeleteAssetMapping deletes an asset mapping by ID.
func DeleteAssetMapping(tx *sqlx.Tx, id string) error {
	_, err := tx.Exec("DELETE FROM integration_asset_mapping WHERE id = $1", id)
	return err
}
