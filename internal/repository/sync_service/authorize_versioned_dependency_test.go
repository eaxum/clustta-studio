package sync_service

import (
	"path/filepath"
	"testing"

	"clustta/internal/repository"
	"clustta/internal/repository/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestCheckpointTagAssignmentRequiresManageDependencies(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(repository.ProjectSchema); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO role (id, mtime, name) VALUES ('artist-role', 1, 'artist');
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Studio', 'Artist', 'artist', 'artist@example.com', 'artist-role');
	`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	data := ProjectData{AssetCheckpointTags: []models.AssetCheckpointTag{{Id: "assignment"}}}
	err = AuthorizeProjectDataWrite(tx, "artist", false, data)
	if err == nil {
		t.Fatal("expected checkpoint tag permission error")
	}

	if _, err = tx.Exec("UPDATE role SET manage_dependencies = 1 WHERE id = 'artist-role'"); err != nil {
		t.Fatal(err)
	}
	if err = AuthorizeProjectDataWrite(tx, "artist", false, data); err != nil {
		t.Fatalf("expected manage_dependencies to allow checkpoint tag assignment: %v", err)
	}
}

func TestCheckpointMetadataUpdateRequiresCreateCheckpoint(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	defer db.Close()
	db.MustExec(repository.ProjectSchema)
	db.MustExec(`
		INSERT INTO role (id, mtime, name) VALUES ('artist-role', 1, 'artist');
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Studio', 'Artist', 'artist', 'artist@example.com', 'artist-role');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id)
		VALUES ('output', 1, 1, 'Output', '.fbx', 'status', 'type');
		INSERT INTO asset_checkpoint
			(id, created_at, mtime, asset_id, xxhash_checksum, time_modified, file_size, chunks, author_id)
		VALUES ('output-v1', 1, 1, 'output', 'hash', 1, 1, '', 'artist');
	`)
	tx := db.MustBegin()
	defer tx.Rollback()
	data := ProjectData{AssetsCheckpoints: []models.Checkpoint{{Id: "output-v1", MTime: 2}}}
	if err := AuthorizeProjectDataWrite(tx, "artist", false, data); err == nil {
		t.Fatal("expected checkpoint update permission error")
	}
	if _, err := tx.Exec("UPDATE role SET create_checkpoint = 1 WHERE id = 'artist-role'"); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeProjectDataWrite(tx, "artist", false, data); err != nil {
		t.Fatalf("expected create_checkpoint to allow metadata update: %v", err)
	}
}

func TestCheckpointPreviewCanUpdateAssetThumbnail(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	defer db.Close()
	db.MustExec(repository.ProjectSchema)
	db.MustExec(`
		INSERT INTO role (id, mtime, name, create_checkpoint) VALUES ('artist-role', 1, 'artist', 1);
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Studio', 'Artist', 'artist', 'artist@example.com', 'artist-role');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id)
		VALUES ('asset', 1, 1, 'Asset', '.blend', 'status', 'type');
	`)
	tx := db.MustBegin()
	defer tx.Rollback()

	data := ProjectData{
		Assets: []models.Asset{{
			Id: "asset", MTime: 2, Name: "Asset", Extension: ".blend",
			StatusId: "status", AssetTypeId: "type", PreviewId: "preview",
		}},
		AssetsCheckpoints: []models.Checkpoint{{
			Id: "checkpoint", MTime: 2, AssetId: "asset", PreviewId: "preview",
		}},
	}
	if err := AuthorizeProjectDataWrite(tx, "artist", false, data); err != nil {
		t.Fatalf("expected checkpoint preview to allow thumbnail update: %v", err)
	}
}

func TestCheckpointPushCanUpdateIntegrationBookkeeping(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	defer db.Close()
	db.MustExec(repository.ProjectSchema)
	db.MustExec(`
		INSERT INTO role (id, mtime, name, create_checkpoint) VALUES ('artist-role', 1, 'artist', 1);
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Studio', 'Artist', 'artist', 'artist@example.com', 'artist-role');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id)
		VALUES ('asset', 1, 1, 'Asset', '.blend', 'status', 'type');
		INSERT INTO integration_asset_mapping
			(id, mtime, integration_id, external_id, external_name, asset_id)
		VALUES ('mapping', 1, 'kitsu', 'external', 'Asset', 'asset');
	`)
	tx := db.MustBegin()
	defer tx.Rollback()

	checkpoint := models.Checkpoint{Id: "checkpoint", MTime: 2, AssetId: "asset"}
	mapping := models.IntegrationAssetMapping{
		Id: "mapping", MTime: 2, IntegrationId: "kitsu", ExternalId: "external",
		ExternalName: "Asset", AssetId: "asset", LastPushedCheckpointId: "checkpoint",
		ExternalAssignees: "[]", ExternalMetadata: "{}", SyncedAt: "2026-10-01T00:00:00Z",
	}
	data := ProjectData{
		AssetsCheckpoints:        []models.Checkpoint{checkpoint},
		IntegrationAssetMappings: []models.IntegrationAssetMapping{mapping},
	}
	if err := AuthorizeProjectDataWrite(tx, "artist", false, data); err != nil {
		t.Fatalf("expected checkpoint push bookkeeping to be allowed: %v", err)
	}

	mapping.ExternalName = "Changed"
	data.IntegrationAssetMappings = []models.IntegrationAssetMapping{mapping}
	if err := AuthorizeProjectDataWrite(tx, "artist", false, data); err == nil {
		t.Fatal("expected integration metadata change to require manage_integrations")
	}
}
