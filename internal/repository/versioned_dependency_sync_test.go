package repository

import (
	"path/filepath"
	"testing"

	"clustta/internal/repository/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

const versionedDependencyTestSchema = `
CREATE TABLE asset (id TEXT PRIMARY KEY, trashed BOOLEAN NOT NULL DEFAULT 0);
CREATE TABLE tag (id TEXT PRIMARY KEY);
CREATE TABLE dependency_type (id TEXT PRIMARY KEY);
CREATE TABLE asset_tag (id TEXT PRIMARY KEY, mtime INTEGER, asset_id TEXT, tag_id TEXT, synced BOOLEAN);
CREATE TABLE asset_checkpoint (
    id TEXT PRIMARY KEY, asset_id TEXT NOT NULL, trashed BOOLEAN NOT NULL DEFAULT 0
);
CREATE TABLE asset_checkpoint_tag (
    id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
    tag_id TEXT NOT NULL, checkpoint_id TEXT NOT NULL, synced BOOLEAN NOT NULL DEFAULT 0,
    UNIQUE(asset_id, tag_id)
);
CREATE TABLE asset_dependency (
    id TEXT PRIMARY KEY, mtime INTEGER NOT NULL, asset_id TEXT NOT NULL,
    dependency_id TEXT NOT NULL, dependency_type_id TEXT NOT NULL,
    resolution_mode TEXT NOT NULL DEFAULT 'floating', checkpoint_id TEXT NULL,
    asset_checkpoint_tag_id TEXT NULL, synced BOOLEAN NOT NULL DEFAULT 0,
    UNIQUE(asset_id, dependency_id)
);`

func openVersionedDependencyTestDB(t *testing.T) *sqlx.DB {
	t.Helper()
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(versionedDependencyTestSchema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`
		INSERT INTO asset (id) VALUES ('shot'), ('boy');
		INSERT INTO tag VALUES ('approved-tag');
		INSERT INTO dependency_type VALUES ('default');
		INSERT INTO asset_checkpoint (id, asset_id) VALUES ('boy-v1', 'boy'), ('shot-v1', 'shot');
	`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSaveCheckpointTagAndDependencySelectors(t *testing.T) {
	db := openVersionedDependencyTestDB(t)
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	assignment := models.AssetCheckpointTag{
		Id: "approved", MTime: 1, AssetId: "boy", TagId: "approved-tag", CheckpointId: "boy-v1",
	}
	if err = SaveAssetCheckpointTag(tx, assignment); err != nil {
		t.Fatal(err)
	}
	assignmentId := assignment.Id
	tagged := models.AssetDependency{
		Id: "edge", MTime: 1, AssetId: "shot", DependencyId: "boy", DependencyTypeId: "default",
		ResolutionMode: DependencyResolutionTagged, AssetCheckpointTagId: &assignmentId,
	}
	if err = SaveDependency(tx, tagged); err != nil {
		t.Fatal(err)
	}

	checkpointId := "boy-v1"
	pinned := tagged
	pinned.MTime = 2
	pinned.ResolutionMode = DependencyResolutionPinned
	pinned.AssetCheckpointTagId = nil
	pinned.CheckpointId = &checkpointId
	if err = SaveDependency(tx, pinned); err != nil {
		t.Fatal(err)
	}

	stored := models.AssetDependency{}
	if err = tx.Get(&stored, "SELECT * FROM asset_dependency WHERE id = 'edge'"); err != nil {
		t.Fatal(err)
	}
	if stored.ResolutionMode != DependencyResolutionPinned || stored.CheckpointId == nil || *stored.CheckpointId != checkpointId {
		t.Fatalf("unexpected stored dependency: %+v", stored)
	}
}

func TestSaveDependencyRejectsCheckpointFromAnotherAsset(t *testing.T) {
	db := openVersionedDependencyTestDB(t)
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	checkpointId := "shot-v1"
	err = SaveDependency(tx, models.AssetDependency{
		Id: "edge", MTime: 1, AssetId: "shot", DependencyId: "boy", DependencyTypeId: "default",
		ResolutionMode: DependencyResolutionPinned, CheckpointId: &checkpointId,
	})
	if err == nil {
		t.Fatal("expected ownership validation error")
	}
}

func TestSaveCheckpointTagRejectsConflictingAssignmentId(t *testing.T) {
	db := openVersionedDependencyTestDB(t)
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	first := models.AssetCheckpointTag{
		Id: "first", MTime: 1, AssetId: "boy", TagId: "approved-tag", CheckpointId: "boy-v1",
	}
	if err = SaveAssetCheckpointTag(tx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Id = "second"
	second.MTime = 2
	if err = SaveAssetCheckpointTag(tx, second); err == nil {
		t.Fatal("expected conflicting assignment ID to be rejected")
	}
}

func TestAssetDependencyProtobufRoundTripPreservesSelector(t *testing.T) {
	checkpointId := "boy-v1"
	original := models.AssetDependency{
		Id: "edge", MTime: 2, AssetId: "shot", DependencyId: "boy", DependencyTypeId: "default",
		ResolutionMode: DependencyResolutionPinned, CheckpointId: &checkpointId,
	}
	roundTrip := FromPbAssetDependencies(ToPbAssetDependencies([]models.AssetDependency{original}))[0]
	if roundTrip.ResolutionMode != original.ResolutionMode || roundTrip.CheckpointId == nil || *roundTrip.CheckpointId != checkpointId {
		t.Fatalf("selector did not round trip: %+v", roundTrip)
	}

	assignment := models.AssetCheckpointTag{
		Id: "approved", MTime: 2, AssetId: "boy", TagId: "approved-tag", CheckpointId: checkpointId,
	}
	assignmentRoundTrip := FromPbAssetCheckpointTags(ToPbAssetCheckpointTags([]models.AssetCheckpointTag{assignment}))[0]
	if assignmentRoundTrip != assignment {
		t.Fatalf("checkpoint tag did not round trip: %+v", assignmentRoundTrip)
	}
}

func TestProjectSchemaCreatesVersionedDependencyTables(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "schema.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(ProjectSchema); err != nil {
		t.Fatal(err)
	}

	for _, tableName := range []string{"asset_dependency", "asset_checkpoint_tag"} {
		var count int
		if err = db.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, tableName); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected table %s", tableName)
		}
	}
}

func TestCheckpointTagIdentityAndStaleWrites(t *testing.T) {
	db := openVersionedDependencyTestDB(t)
	tx := db.MustBegin()
	defer tx.Rollback()
	assignment := models.AssetCheckpointTag{Id: "assignment", MTime: 2, AssetId: "boy", TagId: "approved-tag", CheckpointId: "boy-v1"}
	if err := SaveAssetCheckpointTag(tx, assignment); err != nil {
		t.Fatal(err)
	}
	changed := assignment
	changed.AssetId, changed.CheckpointId, changed.MTime = "shot", "shot-v1", 3
	if err := SaveAssetCheckpointTag(tx, changed); err == nil {
		t.Fatal("expected assignment identity change to fail")
	}
	changed = assignment
	changed.CheckpointId, changed.MTime = "deleted-old-checkpoint", 1
	if err := SaveAssetCheckpointTag(tx, changed); err != nil {
		t.Fatalf("stale assignment should be ignored: %v", err)
	}
	changed = assignment
	changed.Id, changed.TagId = "missing-tag", "missing"
	if err := SaveAssetCheckpointTag(tx, changed); err == nil {
		t.Fatal("expected missing project tag to fail with foreign keys disabled")
	}
	var count int
	if err := tx.Get(&count, "SELECT COUNT(*) FROM asset_tag WHERE asset_id = 'boy' AND tag_id = 'approved-tag'"); err != nil || count != 1 {
		t.Fatalf("expected coupled asset tag: count=%d err=%v", count, err)
	}
}

func TestDependencyStaleAndLegacyUpdatesPreservePin(t *testing.T) {
	db := openVersionedDependencyTestDB(t)
	tx := db.MustBegin()
	defer tx.Rollback()
	checkpoint := "boy-v1"
	edge := models.AssetDependency{Id: "edge", MTime: 2, AssetId: "shot", DependencyId: "boy", DependencyTypeId: "default", ResolutionMode: DependencyResolutionPinned, CheckpointId: &checkpoint}
	if err := SaveDependency(tx, edge); err != nil {
		t.Fatal(err)
	}
	stale := edge
	missing := "deleted-checkpoint"
	stale.CheckpointId, stale.MTime = &missing, 1
	if err := SaveDependency(tx, stale); err != nil {
		t.Fatalf("stale pin should be ignored: %v", err)
	}
	legacy := edge
	legacy.ResolutionMode, legacy.CheckpointId, legacy.MTime = "", nil, 3
	if err := SaveDependency(tx, legacy); err == nil {
		t.Fatal("expected missing selector on existing edge to be rejected")
	}
	var stored models.AssetDependency
	if err := tx.Get(&stored, "SELECT * FROM asset_dependency WHERE id = 'edge'"); err != nil {
		t.Fatal(err)
	}
	if stored.CheckpointId == nil || *stored.CheckpointId != checkpoint || stored.MTime != 2 {
		t.Fatalf("pin changed: %+v", stored)
	}
}
