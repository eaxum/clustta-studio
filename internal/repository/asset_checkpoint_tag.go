package repository

import (
	"database/sql"
	"errors"
	"fmt"

	"clustta/internal/repository/models"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// SaveAssetCheckpointTag applies a newer checkpoint-tag assignment from sync.
func SaveAssetCheckpointTag(tx *sqlx.Tx, assignment models.AssetCheckpointTag) error {
	var existing models.AssetCheckpointTag
	err := tx.Get(&existing, "SELECT * FROM asset_checkpoint_tag WHERE id = ?", assignment.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existing.AssetId != assignment.AssetId || existing.TagId != assignment.TagId {
			return errors.New("checkpoint tag assignment identity cannot change")
		}
		if assignment.MTime <= existing.MTime {
			return nil
		}
	}
	var referenceCount int
	if err := tx.Get(&referenceCount, `SELECT COUNT(*) FROM asset, tag WHERE asset.id = ? AND tag.id = ?`, assignment.AssetId, assignment.TagId); err != nil {
		return err
	}
	if referenceCount != 1 {
		return errors.New("checkpoint tag requires an existing asset and project tag")
	}
	var checkpointCount int
	if err := tx.Get(&checkpointCount, `
		SELECT COUNT(*) FROM asset_checkpoint
		WHERE id = ? AND asset_id = ? AND trashed = 0
	`, assignment.CheckpointId, assignment.AssetId); err != nil {
		return err
	}
	if checkpointCount == 0 {
		return errors.New("checkpoint does not belong to the tag asset")
	}

	var existingId string
	err = tx.Get(&existingId, `
		SELECT id FROM asset_checkpoint_tag
		WHERE asset_id = ? AND tag_id = ?
	`, assignment.AssetId, assignment.TagId)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingId != assignment.Id {
		return fmt.Errorf("checkpoint tag assignment conflicts with existing assignment %s", existingId)
	}

	_, err = tx.Exec(`
		INSERT INTO asset_checkpoint_tag (id, mtime, asset_id, tag_id, checkpoint_id, synced)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			mtime = excluded.mtime,
			asset_id = excluded.asset_id,
			tag_id = excluded.tag_id,
			checkpoint_id = excluded.checkpoint_id,
			synced = excluded.synced
		WHERE excluded.mtime > asset_checkpoint_tag.mtime
	`, assignment.Id, assignment.MTime, assignment.AssetId, assignment.TagId,
		assignment.CheckpointId, assignment.Synced)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`
		INSERT INTO asset_tag (id, mtime, asset_id, tag_id, synced)
		SELECT ?, ?, ?, ?, 0
		WHERE NOT EXISTS (SELECT 1 FROM asset_tag WHERE asset_id = ? AND tag_id = ?)
	`, uuid.NewString(), assignment.MTime, assignment.AssetId, assignment.TagId, assignment.AssetId, assignment.TagId)
	return err
}
