package repository

import (
	"clustta/internal/base_service"
	"clustta/internal/repository/models"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

const (
	DependencyResolutionFloating = "floating"
	DependencyResolutionPinned   = "pinned"
	DependencyResolutionTagged   = "tagged"
)

func normalizeDependencyReference(reference *string) *string {
	if reference == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*reference)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ValidateDependencySelector validates selector shape and reference ownership.
func ValidateDependencySelector(tx *sqlx.Tx, dependencyId, resolutionMode string, checkpointId, checkpointTagId *string) error {
	checkpointId = normalizeDependencyReference(checkpointId)
	checkpointTagId = normalizeDependencyReference(checkpointTagId)

	switch resolutionMode {
	case DependencyResolutionFloating:
		if checkpointId != nil || checkpointTagId != nil {
			return errors.New("floating dependencies cannot reference a checkpoint or tag")
		}
	case DependencyResolutionPinned:
		if checkpointId == nil || checkpointTagId != nil {
			return errors.New("pinned dependencies require only a checkpoint_id")
		}
		var count int
		if err := tx.Get(&count, `SELECT COUNT(*) FROM asset_checkpoint WHERE id = ? AND asset_id = ? AND trashed = 0`, *checkpointId, dependencyId); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("checkpoint does not belong to the dependency asset")
		}
	case DependencyResolutionTagged:
		if checkpointId != nil || checkpointTagId == nil {
			return errors.New("tagged dependencies require only an asset_checkpoint_tag_id")
		}
		var count int
		if err := tx.Get(&count, `
			SELECT COUNT(*) FROM asset_checkpoint_tag act
			JOIN asset_checkpoint ac ON ac.id = act.checkpoint_id
			WHERE act.id = ? AND act.asset_id = ? AND ac.trashed = 0
		`, *checkpointTagId, dependencyId); err != nil {
			return err
		}
		if count == 0 {
			return errors.New("tag does not belong to the dependency asset")
		}
	default:
		return fmt.Errorf("invalid dependency resolution mode: %s", resolutionMode)
	}
	return nil
}

// SaveDependency applies a newer dependency edge received through sync.
func SaveDependency(tx *sqlx.Tx, dependency models.AssetDependency) error {
	var existing models.AssetDependency
	err := tx.Get(&existing, "SELECT * FROM asset_dependency WHERE id = ?", dependency.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existing.AssetId != dependency.AssetId || existing.DependencyId != dependency.DependencyId {
			return errors.New("dependency endpoints cannot change")
		}
		if dependency.MTime <= existing.MTime {
			return nil
		}
		if dependency.ResolutionMode == "" {
			return errors.New("dependency resolution mode is required when updating an existing dependency")
		}
	}
	if dependency.ResolutionMode == "" {
		dependency.ResolutionMode = DependencyResolutionFloating
	}
	var referenceCount int
	if err := tx.Get(&referenceCount, `
		SELECT COUNT(*) FROM asset source, asset target, dependency_type
		WHERE source.id = ? AND target.id = ? AND dependency_type.id = ?
	`, dependency.AssetId, dependency.DependencyId, dependency.DependencyTypeId); err != nil {
		return err
	}
	if referenceCount != 1 {
		return errors.New("dependency requires existing assets and a dependency type")
	}
	dependency.CheckpointId = normalizeDependencyReference(dependency.CheckpointId)
	dependency.AssetCheckpointTagId = normalizeDependencyReference(dependency.AssetCheckpointTagId)
	if err := ValidateDependencySelector(tx, dependency.DependencyId, dependency.ResolutionMode, dependency.CheckpointId, dependency.AssetCheckpointTagId); err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO asset_dependency (
			id, mtime, asset_id, dependency_id, dependency_type_id,
			resolution_mode, checkpoint_id, asset_checkpoint_tag_id, synced
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			mtime = excluded.mtime,
			asset_id = excluded.asset_id,
			dependency_id = excluded.dependency_id,
			dependency_type_id = excluded.dependency_type_id,
			resolution_mode = excluded.resolution_mode,
			checkpoint_id = excluded.checkpoint_id,
			asset_checkpoint_tag_id = excluded.asset_checkpoint_tag_id,
			synced = excluded.synced
		WHERE excluded.mtime > asset_dependency.mtime
	`, dependency.Id, dependency.MTime, dependency.AssetId, dependency.DependencyId,
		dependency.DependencyTypeId, dependency.ResolutionMode, dependency.CheckpointId,
		dependency.AssetCheckpointTagId, dependency.Synced)
	return err
}

func AddDependency(tx *sqlx.Tx, id string, assetId string, dependencyId string, dependencyTypeId string) (models.AssetDependency, error) {
	assetDependency := models.AssetDependency{}
	params := map[string]any{
		"id":                 id,
		"asset_id":           assetId,
		"dependency_id":      dependencyId,
		"dependency_type_id": dependencyTypeId,
	}
	err := base_service.Create(tx, "asset_dependency", params)
	if err != nil {
		return assetDependency, err
	}
	conditions := map[string]any{
		"asset_id":      assetId,
		"dependency_id": dependencyId,
	}
	err = base_service.GetBy(tx, "asset_dependency", conditions, &assetDependency)
	if err != nil {
		return assetDependency, err
	}
	return assetDependency, nil
}

func GetDependency(tx *sqlx.Tx, id string) (models.AssetDependency, error) {
	dependency := models.AssetDependency{}
	err := base_service.Get(tx, "asset_dependency", id, &dependency)
	if err != nil {
		return dependency, err
	}
	return dependency, nil
}

func GetAssetDependencies(tx *sqlx.Tx, assetId string) ([]models.AssetDependency, error) {
	assetDependencies := []models.AssetDependency{}
	conditions := map[string]interface{}{
		"asset_id": assetId,
	}
	err := base_service.GetAllBy(tx, "asset_dependency", conditions, &assetDependencies)
	if err != nil {
		return assetDependencies, err
	}
	return assetDependencies, nil
}
func RemoveAssetDependency(tx *sqlx.Tx, assetId string, dependencyId string) error {
	assetDependency := models.AssetDependency{}
	conditions := map[string]interface{}{
		"asset_id":      assetId,
		"dependency_id": dependencyId,
	}
	err := base_service.DeleteBy(tx, "asset_dependency", conditions)
	if err != nil {
		return err
	}
	err = base_service.GetBy(tx, "asset_dependency", conditions, &assetDependency)
	if err == nil {
		return errors.New("dependency failed to remove")
	} else if err != sql.ErrNoRows {
		return err
	}
	return nil
}
