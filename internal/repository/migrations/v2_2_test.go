package migrations

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

const versionedDependencyMigrationSchema = `
CREATE TABLE IF NOT EXISTS config (name TEXT PRIMARY KEY, value TEXT NOT NULL, mtime INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS asset_dependency (
    id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
    dependency_id TEXT NOT NULL, dependency_type_id TEXT NOT NULL,
    resolution_mode TEXT DEFAULT 'floating' NOT NULL, checkpoint_id TEXT NULL,
    asset_checkpoint_tag_id TEXT NULL, synced BOOLEAN DEFAULT 0 NOT NULL
);
CREATE TABLE IF NOT EXISTS asset_checkpoint (
    id TEXT PRIMARY KEY, created_at DATETIME NOT NULL, asset_id TEXT NOT NULL,
    group_id TEXT DEFAULT '' NOT NULL, trashed BOOLEAN DEFAULT 0 NOT NULL
);
CREATE TABLE IF NOT EXISTS asset_checkpoint_tag (
    id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
    tag_id TEXT NOT NULL, checkpoint_id TEXT NOT NULL, synced BOOLEAN DEFAULT 0 NOT NULL,
    UNIQUE (asset_id, tag_id)
);
DROP VIEW IF EXISTS asset_dependencies;
CREATE VIEW asset_dependencies AS SELECT asset_id, checkpoint_id, asset_checkpoint_tag_id FROM asset_dependency;
DROP VIEW IF EXISTS full_asset;
CREATE VIEW full_asset AS SELECT asset_id FROM asset_dependencies;`

func TestMigrateV2_2AddsVersionedDependenciesAndCheckpointTags(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE config (name TEXT PRIMARY KEY, value TEXT NOT NULL, mtime INTEGER NOT NULL);
		CREATE TABLE asset_dependency (
			id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
			dependency_id TEXT NOT NULL, dependency_type_id TEXT NOT NULL,
			synced BOOLEAN DEFAULT 0 NOT NULL
		);
		CREATE TABLE asset_checkpoint (
			id TEXT PRIMARY KEY, created_at DATETIME NOT NULL, asset_id TEXT NOT NULL,
			group_id TEXT DEFAULT '' NOT NULL, trashed BOOLEAN DEFAULT 0 NOT NULL
		);
		CREATE VIEW asset_dependencies AS SELECT asset_id FROM asset_dependency;
		CREATE VIEW full_asset AS SELECT asset_id FROM asset_dependencies;
		INSERT INTO config (name, value, mtime) VALUES ('version', '2.0', 1);
		INSERT INTO asset_dependency (id, mtime, asset_id, dependency_id, dependency_type_id)
		VALUES ('edge', 1, 'shot', 'boy', 'default');
	`)
	if err != nil {
		t.Fatal(err)
	}

	if err = RunMigrations(db, 2.0, versionedDependencyMigrationSchema); err != nil {
		t.Fatal(err)
	}

	var mode string
	if err = db.Get(&mode, "SELECT resolution_mode FROM asset_dependency WHERE id = 'edge'"); err != nil {
		t.Fatal(err)
	}
	if mode != "floating" {
		t.Fatalf("expected floating migration default, got %s", mode)
	}

	var tableCount int
	if err = db.Get(&tableCount, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'asset_checkpoint_tag'`); err != nil {
		t.Fatal(err)
	}
	if tableCount != 1 {
		t.Fatal("expected asset_checkpoint_tag table")
	}

	var version string
	if err = db.Get(&version, "SELECT value FROM config WHERE name = 'version'"); err != nil {
		t.Fatal(err)
	}
	if version != "2.2" {
		t.Fatalf("expected schema version 2.2, got %s", version)
	}
}

func TestOlderMigrationCanApplyCurrentSelectorIndexes(t *testing.T) {
	schema, err := os.ReadFile("../schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	db := sqlx.MustOpen("sqlite3", filepath.Join(t.TempDir(), "old.clst"))
	defer db.Close()
	db.MustExec(`CREATE TABLE asset_dependency (
		id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
		dependency_id TEXT NOT NULL, dependency_type_id TEXT NOT NULL, synced BOOLEAN DEFAULT 0 NOT NULL);
		INSERT INTO asset_dependency VALUES ('edge', 1, 'shot', 'boy', 'default', 1);`)
	if err := RunMigrations(db, 1.9, string(schema)); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := db.Get(&mode, "SELECT resolution_mode FROM asset_dependency WHERE id = 'edge'"); err != nil || mode != "floating" {
		t.Fatalf("migration lost floating edge: %s, %v", mode, err)
	}
}
