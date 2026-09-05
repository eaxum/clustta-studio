package repository

import (
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestAddItemsToTombDeletesDependencyBeforeCheckpointTag(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE tomb (id TEXT PRIMARY KEY, mtime INTEGER, table_name TEXT, synced BOOLEAN);
		CREATE TABLE asset_checkpoint_tag (id TEXT PRIMARY KEY);
		CREATE TABLE asset_dependency (id TEXT PRIMARY KEY, asset_checkpoint_tag_id TEXT);
		CREATE TRIGGER protect_checkpoint_tag BEFORE DELETE ON asset_checkpoint_tag
		WHEN EXISTS (SELECT 1 FROM asset_dependency WHERE asset_checkpoint_tag_id = OLD.id)
		BEGIN SELECT RAISE(ABORT, 'checkpoint tag is referenced by a dependency'); END;
		INSERT INTO asset_checkpoint_tag (id) VALUES ('assignment');
		INSERT INTO asset_dependency (id, asset_checkpoint_tag_id) VALUES ('edge', 'assignment');
	`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	tombs := []Tomb{
		{Id: "assignment", TableName: "asset_checkpoint_tag"},
		{Id: "edge", TableName: "asset_dependency"},
	}
	if err = AddItemsToTomb(tx, tombs); err != nil {
		t.Fatal(err)
	}

	var remaining int
	if err = tx.Get(&remaining, `
		SELECT (SELECT COUNT(*) FROM asset_dependency) +
		       (SELECT COUNT(*) FROM asset_checkpoint_tag)
	`); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("expected both records deleted, got %d", remaining)
	}
}
