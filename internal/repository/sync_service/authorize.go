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
	checkpointsById := make(map[string]bool, len(localCheckpoints))
	for _, c := range localCheckpoints {
		checkpointsById[c.Id] = true
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

	// Project preview (project-wide config) → admin only
	if data.ProjectPreview != "" && !isAdmin {
		return deny("project_preview", "update", "")
	}

	// Roles → admin only (creating/updating role permission rows)
	if len(data.Roles) > 0 && !isAdmin {
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
		if !role.UpdateAsset {
			return deny("asset", "update", a.Id)
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
		if local.AssigneeId != a.AssigneeId {
			if a.AssigneeId == "" {
				if !role.UnassignAsset {
					return deny("asset", "unassign", a.Id)
				}
			} else if !role.AssignAsset {
				return deny("asset", "assign", a.Id)
			}
		}
	}

	// Asset checkpoints: only creation is meaningful (immutable rows)
	for _, cp := range data.AssetsCheckpoints {
		if checkpointsById[cp.Id] {
			continue
		}
		if !role.CreateCheckpoint {
			return deny("checkpoint", "create", cp.Id)
		}
	}

	// Asset / collection dependencies → ManageDependencies
	for _, d := range data.AssetDependencies {
		if _, err := repository.GetDependency(tx, d.Id); err != nil && !role.ManageDependencies {
			return deny("asset_dependency", "create", d.Id)
		}
	}
	for _, d := range data.CollectionDependencies {
		if _, err := repository.GetCollectionDependency(tx, d.Id); err != nil && !role.ManageDependencies {
			return deny("collection_dependency", "create", d.Id)
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

	// Project-wide config (types, statuses, tags, workflows, integrations) → admin only
	if !isAdmin {
		switch {
		case len(data.CollectionTypes) > 0:
			return deny("collection_type", "modify", "")
		case len(data.AssetTypes) > 0:
			return deny("asset_type", "modify", "")
		case len(data.DependencyTypes) > 0:
			return deny("dependency_type", "modify", "")
		case len(data.Statuses) > 0:
			return deny("status", "modify", "")
		case len(data.Tags) > 0:
			return deny("tag", "modify", "")
		case len(data.Workflows) > 0:
			return deny("workflow", "modify", "")
		case len(data.WorkflowLinks) > 0:
			return deny("workflow_link", "modify", "")
		case len(data.WorkflowCollections) > 0:
			return deny("workflow_collection", "modify", "")
		case len(data.WorkflowAssets) > 0:
			return deny("workflow_asset", "modify", "")
		case len(data.IntegrationProjects) > 0:
			return deny("integration_project", "modify", "")
		case len(data.IntegrationCollectionMappings) > 0:
			return deny("integration_collection_mapping", "modify", "")
		case len(data.IntegrationAssetMappings) > 0:
			return deny("integration_asset_mapping", "modify", "")
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
	case "asset_dependency", "collection_dependency":
		if !role.ManageDependencies {
			return deny(t.TableName, "delete", t.Id)
		}
	case "asset_tag":
		if !role.UpdateAsset {
			return deny("asset_tag", "delete", t.Id)
		}
	default:
		// role, status, tag, collection_type, asset_type, dependency_type,
		// workflow*, integration* — project-wide config, admin only.
		if !isAdmin {
			return deny(t.TableName, "delete", t.Id)
		}
	}
	return nil
}
