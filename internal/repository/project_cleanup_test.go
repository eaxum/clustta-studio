package repository

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestClearTrashRemovesVersionedReferencesBeforeCheckpoint(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ProjectSchema); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO tag (id, mtime, name) VALUES ('approved-tag', 1, 'approved');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id, trashed)
		VALUES ('shot', 1, 1, 'Shot', 'blend', 'status', 'type', 0),
		       ('boy', 1, 1, 'Boy', 'blend', 'status', 'type', 1);
		INSERT INTO asset_checkpoint (
			id, created_at, mtime, asset_id, xxhash_checksum, time_modified,
			file_size, chunks, author_id
		) VALUES ('boy-v1', 1, 1, 'boy', 'hash', 1, 1, 'chunk', 'artist');
		INSERT INTO asset_checkpoint_tag (id, mtime, asset_id, tag_id, checkpoint_id)
		VALUES ('assignment', 1, 'boy', 'approved-tag', 'boy-v1');
		INSERT INTO asset_dependency (
			id, mtime, asset_id, dependency_id, dependency_type_id,
			resolution_mode, asset_checkpoint_tag_id
		) VALUES ('edge', 1, 'shot', 'boy', 'default', 'tagged', 'assignment');
	`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = ClearTrash(tx); err != nil {
		t.Fatal(err)
	}

	for _, tableName := range []string{"asset_dependency", "asset_checkpoint_tag", "asset_checkpoint"} {
		var count int
		if err = tx.Get(&count, "SELECT COUNT(*) FROM "+tableName); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("expected %s to be cleared", tableName)
		}
	}
}

func TestOrphanCleanupCommitsVersionedReferences(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "orphan.clst")
	db := sqlx.MustOpen("sqlite3", projectPath)
	defer db.Close()
	db.MustExec(ProjectSchema)
	db.MustExec(`
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id, collection_id)
		VALUES ('boy', 1, 1, 'Boy', '.blend', 'status', 'type', 'missing');
		INSERT INTO asset_checkpoint (id, created_at, mtime, asset_id, xxhash_checksum, time_modified, file_size, chunks, author_id)
		VALUES ('cp', 1, 1, 'boy', 'hash', 1, 0, '', 'artist');
		INSERT INTO asset_checkpoint_tag (id, mtime, asset_id, tag_id, checkpoint_id)
		VALUES ('assignment', 1, 'boy', 'tag', 'cp');
	`)
	if err := ClearProjectOrphans(projectPath); err != nil {
		t.Fatal(err)
	}
	for _, tableName := range []string{"asset", "asset_checkpoint", "asset_checkpoint_tag"} {
		var count int
		if err := db.Get(&count, "SELECT COUNT(*) FROM "+tableName); err != nil || count != 0 {
			t.Fatalf("%s not cleared: count=%d err=%v", tableName, count, err)
		}
	}
}
