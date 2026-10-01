package sync_service

import (
	"clustta/internal/compatibility"
	"clustta/internal/repository/repositorypb"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/protobuf/proto"
)

func TestProjectDataBytesForAPI1OmitsAPI2Fields(t *testing.T) {
	source := "source"
	data := &repositorypb.ProjectData{
		AssetDependencies: []*repositorypb.AssetDependency{{
			Id: "dependency", ResolutionMode: "pinned", CheckpointId: "checkpoint",
		}},
		AssetsCheckpoints:   []*repositorypb.Checkpoint{{Id: "checkpoint", SourceCheckpointId: &source}},
		AssetCheckpointTags: []*repositorypb.AssetCheckpointTag{{Id: "assignment"}},
		Roles:               []*repositorypb.Role{{Id: "role", ManageRoles: true}},
		Tomb:                []*repositorypb.Tomb{{Id: "tomb", TableName: "asset_checkpoint_tag"}},
	}
	encoded, err := proto.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	projected, err := ProjectDataBytesForAPI(encoded, compatibility.LegacyAPIVersion)
	if err != nil {
		t.Fatal(err)
	}
	var result repositorypb.ProjectData
	if err = proto.Unmarshal(projected, &result); err != nil {
		t.Fatal(err)
	}
	if result.AssetDependencies[0].ResolutionMode != "" ||
		result.AssetsCheckpoints[0].SourceCheckpointId != nil ||
		len(result.AssetCheckpointTags) != 0 ||
		len(result.Tomb) != 0 ||
		result.Roles[0].ManageRoles {
		t.Fatalf("API 2 fields leaked into API 1: %+v", &result)
	}
}

func TestPreserveCanonicalFieldsForAPI1(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", ":memory:")
	defer db.Close()
	db.MustExec(`
		CREATE TABLE asset_dependency (
			id TEXT PRIMARY KEY,
			resolution_mode TEXT,
			checkpoint_id TEXT,
			asset_checkpoint_tag_id TEXT
		);
		CREATE TABLE asset_checkpoint (id TEXT PRIMARY KEY, source_checkpoint_id TEXT);
		CREATE TABLE role (
			id TEXT PRIMARY KEY,
			manage_collection_types BOOLEAN,
			manage_asset_types BOOLEAN,
			manage_dependency_types BOOLEAN,
			manage_statuses BOOLEAN,
			manage_tags BOOLEAN,
			manage_workflows BOOLEAN,
			manage_integrations BOOLEAN,
			manage_project_settings BOOLEAN,
			manage_roles BOOLEAN
		);
		INSERT INTO asset_dependency VALUES ('dependency', 'pinned', 'checkpoint', NULL);
		INSERT INTO asset_checkpoint VALUES ('checkpoint', 'source');
		INSERT INTO role VALUES ('role', 1, 1, 1, 1, 1, 1, 1, 1, 1);
	`)
	tx := db.MustBegin()
	defer tx.Rollback()
	data := &repositorypb.ProjectData{
		AssetDependencies:   []*repositorypb.AssetDependency{{Id: "dependency"}},
		AssetsCheckpoints:   []*repositorypb.Checkpoint{{Id: "checkpoint"}},
		AssetCheckpointTags: []*repositorypb.AssetCheckpointTag{{Id: "unknown-to-api-1"}},
		Roles:               []*repositorypb.Role{{Id: "role"}},
	}
	if err := PreserveCanonicalFieldsForAPI(tx, compatibility.LegacyAPIVersion, data); err != nil {
		t.Fatal(err)
	}
	if data.AssetDependencies[0].ResolutionMode != "pinned" ||
		data.AssetsCheckpoints[0].SourceCheckpointId == nil ||
		!data.Roles[0].ManageRoles ||
		len(data.AssetCheckpointTags) != 0 {
		t.Fatalf("canonical fields were not preserved: %+v", data)
	}
}
