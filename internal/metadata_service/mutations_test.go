package metadata_service

import (
	"clustta/internal/repository"
	"clustta/internal/utils"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestAssetPatchAssigneeNullMeansUnassign(t *testing.T) {
	var patch AssetPatch
	if err := json.Unmarshal([]byte(`{"id":"asset-1","assignee_id":null}`), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.AssigneeId == nil || *patch.AssigneeId != "" {
		t.Fatalf("expected explicit null to decode as unassign, got %#v", patch.AssigneeId)
	}
}

func TestTypeMutationReturnsTokenPredecessor(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.clst"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(repository.ProjectSchema); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO config(name,value,mtime) VALUES('sync_token','before',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO role(id,mtime,name,synced) VALUES('admin-role',1,'admin',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO user(id,mtime,added_at,first_name,last_name,username,email,role_id,synced)
		VALUES('admin-user',1,'now','Admin','User','admin','admin@example.com','admin-role',1)`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	response, err := PutAssetType(tx, "admin-user", TypePutRequest{Id: "type-1", Name: "Animation", Icon: "animation"})
	if err != nil {
		t.Fatal(err)
	}
	if response.PreviousSyncToken != "before" || response.SyncToken == "" || response.SyncToken == "before" {
		t.Fatalf("unexpected token chain: %#v", response)
	}
	stored, err := utils.GetProjectSyncToken(tx)
	if err != nil {
		t.Fatal(err)
	}
	if stored != response.SyncToken {
		t.Fatalf("stored token %q does not match response %q", stored, response.SyncToken)
	}
}

func TestAssetPatchOmittedAssigneeMeansUnchanged(t *testing.T) {
	var patch AssetPatch
	if err := json.Unmarshal([]byte(`{"id":"asset-1"}`), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.AssigneeId != nil {
		t.Fatalf("expected omitted assignee to remain nil, got %#v", patch.AssigneeId)
	}
}

func TestTypeAssignmentFieldsDecode(t *testing.T) {
	var asset AssetPatch
	if err := json.Unmarshal([]byte(`{"id":"asset-1","asset_type_id":"type-1"}`), &asset); err != nil {
		t.Fatal(err)
	}
	if asset.AssetTypeId == nil || *asset.AssetTypeId != "type-1" {
		t.Fatalf("unexpected asset type: %#v", asset.AssetTypeId)
	}

	var collection CollectionPatch
	if err := json.Unmarshal([]byte(`{"id":"collection-1","collection_type_id":"type-2"}`), &collection); err != nil {
		t.Fatal(err)
	}
	if collection.CollectionTypeId == nil || *collection.CollectionTypeId != "type-2" {
		t.Fatalf("unexpected collection type: %#v", collection.CollectionTypeId)
	}
}
