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
