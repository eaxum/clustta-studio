package sync_service

import (
	"clustta/internal/repository"
	"clustta/internal/repository/models"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// PermissionError is returned when a sync push attempts an operation the
// caller's project role does not allow. Handlers should map this to HTTP 403.
type PermissionError struct {
	Entity string
	Op     string
	Id     string
}

func (e *PermissionError) Error() string {
	return fmt.Sprintf("permission denied: %s %s (id=%s)", e.Op, e.Entity, e.Id)
}

func deny(entity, op, id string) error {
	return &PermissionError{Entity: entity, Op: op, Id: id}
}

// AuthorizeProjectDataWrite verifies the caller is allowed to perform every
// mutation implied by the supplied ProjectData. Permissions are read from the
// project's own .clst (server-side ground truth); the payload's Roles/Users
// arrays are NOT trusted as a source of identity.
//
// If bypass is true (project owner or studio admin) all checks are skipped.
// On the first violation the function returns a *PermissionError; otherwise nil.
func AuthorizeProjectDataWrite(tx *sqlx.Tx, callerUserId string, bypass bool, data ProjectData) error {
	if bypass {
		return nil
	}

	caller, err := repository.GetUser(tx, callerUserId)
	if err != nil {
		return deny("project", "access", callerUserId)
	}
	role, err := repository.GetRole(tx, caller.RoleId)
	if err != nil {
		return deny("project", "access", callerUserId)
	}
	isAdmin := role.Name == "admin"

	// Build local indexes once for diff classification
	localCollections, err := repository.GetSimpleCollections(tx)
	if err != nil {
		return err
	}
	collectionsById := make(map[string]models.Collection, len(localCollections))
	for _, c := range localCollections {
		collectionsById[c.Id] = c
	}

	localAssets, err := repository.GetSimpleAssets(tx)
	if err != nil {
		return err
	}
	assetsById := make(map[string]models.Asset, len(localAssets))
	for _, a := range localAssets {
		assetsById[a.Id] = a
	}

	localCheckpoints, err := repository.GetSimpleCheckpoints(tx)
	if err != nil {
		return err
	}
	checkpointsById := make(map[string]models.Checkpoint, len(localCheckpoints))
	for _, c := range localCheckpoints {
		checkpointsById[c.Id] = c
	}

	// Helper: look up status name for SetDone/SetRetake gating.
	statusName := func(id string) string {
		if id == "" {
			return ""
		}
		s, err := repository.GetStatus(tx, id)
		if err != nil {
			return ""
		}
		return s.Name
	}

	// Project-wide settings.
	if data.ProjectPreview != "" && !role.ManageProjectSettings {
		return deny("project_preview", "update", "")
	}
	if len(data.ProjectConfigs) > 0 && !role.ManageProjectSettings {
		return deny("project_config", "update", "")
	}

	// Role definitions.
	if len(data.Roles) > 0 && !role.ManageRoles {
		return deny("role", "modify", "")
	}

	// Users: new rows = AddUser; role_id diff = ChangeRole; never allow self-elevation
	for _, u := range data.Users {
		local, err := repository.GetUser(tx, u.Id)
		if err != nil {
			if !role.AddUser {
				return deny("user", "add", u.Id)
			}
			continue
		}
		if local.RoleId == u.RoleId {
			continue
		}
		if u.Id == callerUserId {
			return deny("user", "self_elevate", u.Id)
		}
		if !role.ChangeRole {
			return deny("user", "change_role", u.Id)
		}
	}

	// Collections: create / update / (delete handled in tomb pass)
	for _, c := range data.Collections {
		local, exists := collectionsById[c.Id]
		if !exists {
			if !role.CreateCollection {
				return deny("collection", "create", c.Id)
			}
			continue
		}
		if local.MTime < c.MTime && !role.UpdateCollection {
			return deny("collection", "update", c.Id)
		}
	}

	// Collection assignees: create = AssignAsset
	for _, ca := range data.CollectionAssignees {
		if _, err := repository.GetAssignee(tx, ca.Id); err != nil {
			if !role.AssignAsset {
				return deny("collection_assignee", "create", ca.Id)
			}
		}
	}

	// Assets: create = CreateAsset; update = UpdateAsset + status/assignee sub-checks
	for _, a := range data.Assets {
		local, exists := assetsById[a.Id]
		if !exists {
			if !role.CreateAsset {
				return deny("asset", "create", a.Id)
			}
			continue
		}
		if local.MTime >= a.MTime {
			continue
		}
		if assetMetadataChanged(local, a) && !role.UpdateAsset {
			return deny("asset", "update", a.Id)
		}
		if local.PreviewId != a.PreviewId && !role.UpdateAsset && !checkpointPreviewChangeAllowed(a, data.AssetsCheckpoints, role) {
			return deny("asset", "update_preview", a.Id)
		}
		if local.StatusId != a.StatusId {
			if !role.ChangeStatus {
				return deny("asset", "change_status", a.Id)
			}
			switch statusName(a.StatusId) {
			case "done":
				if !role.SetDoneAsset {
					return deny("asset", "set_done", a.Id)
				}
			case "retake":
				if !role.SetRetakeAsset {
					return deny("asset", "set_retake", a.Id)
				}
			}
		}
		if local.AssigneeId != a.AssigneeId || local.AssignerId != a.AssignerId {
			if a.AssigneeId == "" {
				if !role.UnassignAsset {
					return deny("asset", "unassign", a.Id)
				}
			} else if !role.AssignAsset {
				return deny("asset", "assign", a.Id)
			}
		}
	}

	// Asset checkpoints use CreateCheckpoint for creation and metadata edits.
	for _, cp := range data.AssetsCheckpoints {
		local, exists := checkpointsById[cp.Id]
		if exists && local.MTime >= cp.MTime {
			continue
		}
		if !role.CreateCheckpoint {
			op := "create"
			if exists {
				op = "update"
			}
			return deny("checkpoint", op, cp.Id)
		}
	}

	// Asset / collection dependencies → ManageDependencies
	for _, d := range data.AssetDependencies {
		if !role.ManageDependencies {
			return deny("asset_dependency", "modify", d.Id)
		}
	}
	for _, d := range data.CollectionDependencies {
		if !role.ManageDependencies {
			return deny("collection_dependency", "modify", d.Id)
		}
	}
	for _, assignment := range data.AssetCheckpointTags {
		if !role.ManageDependencies {
			return deny("asset_checkpoint_tag", "modify", assignment.Id)
		}
	}

	// Templates → create/update/delete
	for _, t := range data.Templates {
		if _, err := repository.GetTemplate(tx, t.Id); err != nil && !role.CreateTemplate {
			return deny("template", "create", t.Id)
		}
	}

	// Asset tags → treated as asset edit
	for _, at := range data.AssetsTags {
		if _, err := repository.GetAssetTag(tx, at.Id); err != nil && !role.UpdateAsset {
			return deny("asset_tag", "create", at.Id)
		}
	}

	// Project-wide configuration.
	if len(data.CollectionTypes) > 0 && !role.ManageCollectionTypes {
		return deny("collection_type", "modify", "")
	}
	if len(data.AssetTypes) > 0 && !role.ManageAssetTypes {
		return deny("asset_type", "modify", "")
	}
	if len(data.DependencyTypes) > 0 && !role.ManageDependencyTypes {
		return deny("dependency_type", "modify", "")
	}
	if len(data.Statuses) > 0 && !role.ManageStatuses {
		return deny("status", "modify", "")
	}
	if len(data.Tags) > 0 && !role.ManageTags {
		return deny("tag", "modify", "")
	}
	if (len(data.Workflows) > 0 || len(data.WorkflowLinks) > 0 || len(data.WorkflowCollections) > 0 || len(data.WorkflowAssets) > 0) && !role.ManageWorkflows {
		return deny("workflow", "modify", "")
	}
	if (len(data.IntegrationProjects) > 0 || len(data.IntegrationCollectionMappings) > 0) && !role.ManageIntegrations {
		return deny("integration", "modify", "")
	}
	if !role.ManageIntegrations {
		localMappings, err := repository.GetAllAssetMappings(tx)
		if err != nil {
			return err
		}
		mappingsById := make(map[string]models.IntegrationAssetMapping, len(localMappings))
		for _, mapping := range localMappings {
			mappingsById[mapping.Id] = mapping
		}
		for _, mapping := range data.IntegrationAssetMappings {
			local, exists := mappingsById[mapping.Id]
			if !exists || !integrationCheckpointUpdateAllowed(local, mapping, checkpointsById, data.AssetsCheckpoints, role) {
				return deny("integration_asset_mapping", "modify", mapping.Id)
			}
		}
	}

	// Tombs: classify by table_name, gate on the matching delete permission.
	for _, t := range data.Tombs {
		if err := authorizeTomb(role, isAdmin, t); err != nil {
			return err
		}
	}

	return nil
}

// authorizeTomb checks delete permission for a single tombed item based on
// its table_name. Unknown table names are treated as admin-only to fail closed.
func authorizeTomb(role models.Role, isAdmin bool, t repository.Tomb) error {
	switch t.TableName {
	case "asset":
		if !role.DeleteAsset {
			return deny("asset", "delete", t.Id)
		}
	case "asset_checkpoint":
		if !role.DeleteCheckpoint {
			return deny("checkpoint", "delete", t.Id)
		}
	case "collection":
		if !role.DeleteCollection {
			return deny("collection", "delete", t.Id)
		}
	case "template":
		if !role.DeleteTemplate {
			return deny("template", "delete", t.Id)
		}
	case "collection_assignee":
		if !role.UnassignAsset {
			return deny("collection_assignee", "delete", t.Id)
		}
	case "asset_dependency", "collection_dependency", "asset_checkpoint_tag":
		if !role.ManageDependencies {
			return deny(t.TableName, "delete", t.Id)
		}
	case "asset_tag":
		if !role.UpdateAsset {
			return deny("asset_tag", "delete", t.Id)
		}
	case "collection_type":
		if !role.ManageCollectionTypes {
			return deny(t.TableName, "delete", t.Id)
		}
	case "asset_type":
		if !role.ManageAssetTypes {
			return deny(t.TableName, "delete", t.Id)
		}
	case "dependency_type":
		if !role.ManageDependencyTypes {
			return deny(t.TableName, "delete", t.Id)
		}
	case "status":
		if !role.ManageStatuses {
			return deny(t.TableName, "delete", t.Id)
		}
	case "tag":
		if !role.ManageTags {
			return deny(t.TableName, "delete", t.Id)
		}
	case "workflow", "workflow_link", "workflow_collection", "workflow_asset":
		if !role.ManageWorkflows {
			return deny(t.TableName, "delete", t.Id)
		}
	case "integration_project", "integration_collection_mapping", "integration_asset_mapping":
		if !role.ManageIntegrations {
			return deny(t.TableName, "delete", t.Id)
		}
	case "role":
		if !role.ManageRoles {
			return deny(t.TableName, "delete", t.Id)
		}
	default:
		// Unknown project data is restricted to the reserved admin role.
		if !isAdmin {
			return deny(t.TableName, "delete", t.Id)
		}
	}
	return nil
}

func assetMetadataChanged(local, incoming models.Asset) bool {
	return local.Name != incoming.Name ||
		local.Description != incoming.Description ||
		local.Extension != incoming.Extension ||
		local.IsResource != incoming.IsResource ||
		local.AssetTypeId != incoming.AssetTypeId ||
		local.CollectionId != incoming.CollectionId ||
		local.IsLink != incoming.IsLink ||
		local.Pointer != incoming.Pointer ||
		local.Trashed != incoming.Trashed
}

func checkpointPreviewChangeAllowed(asset models.Asset, checkpoints []models.Checkpoint, role models.Role) bool {
	if !role.CreateCheckpoint || asset.PreviewId == "" {
		return false
	}
	for _, checkpoint := range checkpoints {
		if checkpoint.AssetId == asset.Id && checkpoint.PreviewId == asset.PreviewId {
			return true
		}
	}
	return false
}

func integrationCheckpointUpdateAllowed(local, incoming models.IntegrationAssetMapping, localCheckpoints map[string]models.Checkpoint, incomingCheckpoints []models.Checkpoint, role models.Role) bool {
	if !role.CreateCheckpoint || incoming.LastPushedCheckpointId == "" {
		return false
	}
	if local.IntegrationId != incoming.IntegrationId ||
		local.ExternalId != incoming.ExternalId ||
		local.ExternalName != incoming.ExternalName ||
		local.ExternalParentId != incoming.ExternalParentId ||
		local.ExternalType != incoming.ExternalType ||
		local.ExternalStatus != incoming.ExternalStatus ||
		local.ExternalAssignees != incoming.ExternalAssignees ||
		local.ExternalMetadata != incoming.ExternalMetadata ||
		local.AssetId != incoming.AssetId {
		return false
	}
	if checkpoint, exists := localCheckpoints[incoming.LastPushedCheckpointId]; exists && checkpoint.AssetId == incoming.AssetId {
		return true
	}
	for _, checkpoint := range incomingCheckpoints {
		if checkpoint.Id == incoming.LastPushedCheckpointId && checkpoint.AssetId == incoming.AssetId {
			return true
		}
	}
	return false
}
