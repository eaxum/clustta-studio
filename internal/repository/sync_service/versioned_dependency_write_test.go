package sync_service

import (
	"path/filepath"
	"testing"

	"clustta/internal/repository"
	"clustta/internal/repository/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestWriteProjectDataCreatesAndUpdatesVersionedDependency(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(repository.ProjectSchema); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO tag (id, mtime, name) VALUES ('approved-tag', 1, 'approved');
		INSERT INTO dependency_type (id, mtime, name) VALUES ('default', 1, 'default');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id)
		VALUES ('shot', 1, 1, 'Shot', 'blend', 'status', 'type'),
		       ('boy', 1, 1, 'Boy', 'blend', 'status', 'type');
		INSERT INTO asset_checkpoint (
			id, created_at, mtime, asset_id, xxhash_checksum, time_modified,
			file_size, chunks, author_id
		) VALUES ('boy-v1', 1, 1, 'boy', 'hash', 1, 1, 'chunk', 'artist');
	`)
	if err != nil {
		t.Fatal(err)
	}

	assignmentId := "approved-assignment"
	data := ProjectData{
		AssetCheckpointTags: []models.AssetCheckpointTag{{
			Id: assignmentId, MTime: 1, AssetId: "boy", TagId: "approved-tag", CheckpointId: "boy-v1",
		}},
		AssetDependencies: []models.AssetDependency{{
			Id: "edge", MTime: 1, AssetId: "shot", DependencyId: "boy", DependencyTypeId: "default",
			ResolutionMode: repository.DependencyResolutionTagged, AssetCheckpointTagId: &assignmentId,
		}},
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteProjectData(tx, data, false); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	checkpointId := "boy-v1"
	data.AssetDependencies[0].MTime = 2
	data.AssetDependencies[0].ResolutionMode = repository.DependencyResolutionPinned
	data.AssetDependencies[0].AssetCheckpointTagId = nil
	data.AssetDependencies[0].CheckpointId = &checkpointId
	tx, err = db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = WriteProjectData(tx, data, false); err != nil {
		t.Fatal(err)
	}

	stored := models.AssetDependency{}
	if err = tx.Get(&stored, "SELECT * FROM asset_dependency WHERE id = 'edge'"); err != nil {
		t.Fatal(err)
	}
	if stored.ResolutionMode != repository.DependencyResolutionPinned {
		t.Fatalf("expected pinned selector update, got %+v", stored)
	}
	changed, err := LoadChangedData(tx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.AssetCheckpointTags) != 1 || len(changed.AssetDependencies) != 1 {
		t.Fatalf("expected versioned dependency changes, got %+v", changed)
	}
	if _, err := tx.Exec("UPDATE asset_checkpoint SET trashed = 1 WHERE id = 'boy-v1'"); err == nil {
		t.Fatal("expected referenced checkpoint trash to fail")
	}
	if _, err := tx.Exec("DELETE FROM asset_checkpoint_tag WHERE id = ?", assignmentId); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DELETE FROM asset_checkpoint WHERE id = 'boy-v1'"); err == nil {
		t.Fatal("expected exact pin alone to protect checkpoint deletion")
	}
	var count int
	if err := tx.Get(&count, "SELECT COUNT(*) FROM asset_tag WHERE asset_id = 'boy'"); err != nil || count != 0 {
		t.Fatalf("checkpoint assignment removal must remove asset tag: count=%d err=%v", count, err)
	}
}
