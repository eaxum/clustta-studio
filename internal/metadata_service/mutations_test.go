package metadata_service

import (
	"encoding/json"
	"testing"
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
