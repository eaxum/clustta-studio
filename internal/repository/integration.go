package repository

import (
	"clustta/internal/base_service"
	"clustta/internal/repository/models"
	"clustta/internal/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// CreateIntegrationProject creates a new integration project link.
func CreateIntegrationProject(tx *sqlx.Tx, integrationType, externalProjectId, externalProjectName, apiUrl, config string) (models.IntegrationProject, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_project (id, mtime, integration_type, external_project_id, external_project_name, api_url, config, synced)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0)`
	_, err := tx.Exec(query, id, mtime, integrationType, externalProjectId, externalProjectName, apiUrl, config)
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

// GetIntegrationProjectByType retrieves an integration project by integration type.
func GetIntegrationProjectByType(tx *sqlx.Tx, integrationType string) (models.IntegrationProject, error) {
	var project models.IntegrationProject
	err := tx.Get(&project, "SELECT * FROM integration_project WHERE integration_type = $1", integrationType)
	return project, err
}

// GetIntegrationProjects retrieves all integration projects.
func GetIntegrationProjects(tx *sqlx.Tx) ([]models.IntegrationProject, error) {
	var projects []models.IntegrationProject
	err := base_service.GetAll(tx, "integration_project", &projects)
	return projects, err
}

// UpdateIntegrationProject updates an existing integration project.
func UpdateIntegrationProject(tx *sqlx.Tx, id, externalProjectId, externalProjectName, apiUrl, config string) (models.IntegrationProject, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_project SET mtime = $1, external_project_id = $2, external_project_name = $3, api_url = $4, config = $5 WHERE id = $6`
	_, err := tx.Exec(query, mtime, externalProjectId, externalProjectName, apiUrl, config, id)
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
func CreateCollectionMapping(tx *sqlx.Tx, integrationProjectId, collectionId, externalEntityId, externalEntityType string) (models.IntegrationCollectionMapping, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_collection_mapping (id, mtime, integration_project_id, collection_id, external_entity_id, external_entity_type, synced)
		VALUES ($1, $2, $3, $4, $5, $6, 0)`
	_, err := tx.Exec(query, id, mtime, integrationProjectId, collectionId, externalEntityId, externalEntityType)
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

// GetCollectionMappingByCollectionId retrieves a collection mapping by collection ID and integration project.
func GetCollectionMappingByCollectionId(tx *sqlx.Tx, integrationProjectId, collectionId string) (models.IntegrationCollectionMapping, error) {
	var mapping models.IntegrationCollectionMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_collection_mapping WHERE integration_project_id = $1 AND collection_id = $2", integrationProjectId, collectionId)
	return mapping, err
}

// GetCollectionMappings retrieves all collection mappings for an integration project.
func GetCollectionMappings(tx *sqlx.Tx, integrationProjectId string) ([]models.IntegrationCollectionMapping, error) {
	var mappings []models.IntegrationCollectionMapping
	err := tx.Select(&mappings, "SELECT * FROM integration_collection_mapping WHERE integration_project_id = $1", integrationProjectId)
	return mappings, err
}

// GetAllCollectionMappings retrieves all collection mappings.
func GetAllCollectionMappings(tx *sqlx.Tx) ([]models.IntegrationCollectionMapping, error) {
	var mappings []models.IntegrationCollectionMapping
	err := base_service.GetAll(tx, "integration_collection_mapping", &mappings)
	return mappings, err
}

// UpdateCollectionMapping updates an existing collection mapping.
func UpdateCollectionMapping(tx *sqlx.Tx, id, externalEntityId, externalEntityType string) (models.IntegrationCollectionMapping, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_collection_mapping SET mtime = $1, external_entity_id = $2, external_entity_type = $3 WHERE id = $4`
	_, err := tx.Exec(query, mtime, externalEntityId, externalEntityType, id)
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
func CreateAssetMapping(tx *sqlx.Tx, integrationProjectId, assetId, externalTaskId string) (models.IntegrationAssetMapping, error) {
	id := uuid.New().String()
	mtime := utils.GetEpochTime()

	query := `INSERT INTO integration_asset_mapping (id, mtime, integration_project_id, asset_id, external_task_id, synced)
		VALUES ($1, $2, $3, $4, $5, 0)`
	_, err := tx.Exec(query, id, mtime, integrationProjectId, assetId, externalTaskId)
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

// GetAssetMappingByAssetId retrieves an asset mapping by asset ID and integration project.
func GetAssetMappingByAssetId(tx *sqlx.Tx, integrationProjectId, assetId string) (models.IntegrationAssetMapping, error) {
	var mapping models.IntegrationAssetMapping
	err := tx.Get(&mapping, "SELECT * FROM integration_asset_mapping WHERE integration_project_id = $1 AND asset_id = $2", integrationProjectId, assetId)
	return mapping, err
}

// GetAssetMappings retrieves all asset mappings for an integration project.
func GetAssetMappings(tx *sqlx.Tx, integrationProjectId string) ([]models.IntegrationAssetMapping, error) {
	var mappings []models.IntegrationAssetMapping
	err := tx.Select(&mappings, "SELECT * FROM integration_asset_mapping WHERE integration_project_id = $1", integrationProjectId)
	return mappings, err
}

// GetAllAssetMappings retrieves all asset mappings.
func GetAllAssetMappings(tx *sqlx.Tx) ([]models.IntegrationAssetMapping, error) {
	var mappings []models.IntegrationAssetMapping
	err := base_service.GetAll(tx, "integration_asset_mapping", &mappings)
	return mappings, err
}

// UpdateAssetMapping updates an existing asset mapping.
func UpdateAssetMapping(tx *sqlx.Tx, id, externalTaskId string) (models.IntegrationAssetMapping, error) {
	mtime := utils.GetEpochTime()

	query := `UPDATE integration_asset_mapping SET mtime = $1, external_task_id = $2 WHERE id = $3`
	_, err := tx.Exec(query, mtime, externalTaskId, id)
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
