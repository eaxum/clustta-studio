package metadata_service

import (
	"clustta/internal/error_service"
	"clustta/internal/repository"
	"clustta/internal/repository/models"
	"clustta/internal/utils"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
)

var ErrForbidden = errors.New("forbidden")

type AssetPatch struct {
	Id          string  `json:"id"`
	StatusId    *string `json:"status_id,omitempty"`
	AssigneeId  *string `json:"assignee_id,omitempty"`
	IsTask      *bool   `json:"is_task,omitempty"`
	AssetTypeId *string `json:"asset_type_id,omitempty"`
}

// UnmarshalJSON preserves the distinction between an omitted assignee_id
// (leave unchanged) and an explicit null (unassign).
func (p *AssetPatch) UnmarshalJSON(data []byte) error {
	type assetPatch AssetPatch
	var decoded assetPatch
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*p = AssetPatch(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	raw, present := fields["assignee_id"]
	if !present {
		return nil
	}
	value := ""
	if string(raw) != "null" {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
	}
	p.AssigneeId = &value
	return nil
}

type AssetRequest struct {
	Assets []AssetPatch `json:"assets"`
}
type AssetResponse struct {
	Assets    []models.Asset `json:"assets"`
	SyncToken string         `json:"sync_token"`
}
type CollectionPatch struct {
	Id                string   `json:"id"`
	IsShared          *bool    `json:"is_shared,omitempty"`
	CollectionTypeId  *string  `json:"collection_type_id,omitempty"`
	AddAssigneeIds    []string `json:"add_assignee_ids,omitempty"`
	RemoveAssigneeIds []string `json:"remove_assignee_ids,omitempty"`
}
type CollectionRequest struct {
	Collections []CollectionPatch `json:"collections"`
}
type CollectionResponse struct {
	Collections         []models.Collection         `json:"collections"`
	CollectionAssignees []models.CollectionAssignee `json:"collection_assignees"`
	SyncToken           string                      `json:"sync_token"`
}

type TypePutRequest struct {
	Id   string `json:"-"`
	Name string `json:"name"`
	Icon string `json:"icon"`
}

type AssetTypeResponse struct {
	AssetType models.AssetType `json:"asset_type"`
	SyncToken string           `json:"sync_token"`
}

type CollectionTypeResponse struct {
	CollectionType models.CollectionType `json:"collection_type"`
	SyncToken      string                `json:"sync_token"`
}

func ApplyAssets(tx *sqlx.Tx, actorId string, req AssetRequest) (AssetResponse, error) {
	if len(req.Assets) == 0 {
		return AssetResponse{}, fmt.Errorf("assets array is required")
	}
	actor, err := repository.GetUser(tx, actorId)
	if err != nil {
		return AssetResponse{}, ErrForbidden
	}
	for _, p := range req.Assets {
		if p.Id == "" {
			return AssetResponse{}, fmt.Errorf("asset id is required")
		}
		if _, err = repository.GetSimpleAsset(tx, p.Id); err != nil {
			return AssetResponse{}, err
		}
		if p.StatusId != nil && !actor.Role.ChangeStatus {
			return AssetResponse{}, ErrForbidden
		}
		if p.IsTask != nil && !actor.Role.UpdateAsset {
			return AssetResponse{}, ErrForbidden
		}
		if p.AssetTypeId != nil {
			if !actor.Role.UpdateAsset {
				return AssetResponse{}, ErrForbidden
			}
			if _, err = repository.GetAssetType(tx, *p.AssetTypeId); err != nil {
				return AssetResponse{}, err
			}
		}
		if p.AssigneeId != nil {
			if *p.AssigneeId == "" && !actor.Role.UnassignAsset {
				return AssetResponse{}, ErrForbidden
			}
			if *p.AssigneeId != "" {
				if !actor.Role.AssignAsset {
					return AssetResponse{}, ErrForbidden
				}
				if _, err = repository.GetUser(tx, *p.AssigneeId); err != nil {
					return AssetResponse{}, fmt.Errorf("assignee_not_collaborator: %s", *p.AssigneeId)
				}
			}
		}
		if p.AssigneeId != nil && *p.AssigneeId != "" && p.IsTask != nil && !*p.IsTask {
			return AssetResponse{}, fmt.Errorf("assigned_asset_must_be_task: %s", p.Id)
		}
	}
	out := AssetResponse{Assets: make([]models.Asset, 0, len(req.Assets))}
	for _, p := range req.Assets {
		if p.StatusId != nil {
			if err = repository.UpdateStatus(tx, p.Id, *p.StatusId); err != nil {
				return AssetResponse{}, err
			}
		}
		if p.AssigneeId != nil {
			if err = repository.UpdateAssignation(tx, p.Id, *p.AssigneeId, actorId); err != nil {
				return AssetResponse{}, err
			}
			if *p.AssigneeId != "" {
				if err = repository.ToggleIsAsset(tx, p.Id, true); err != nil {
					return AssetResponse{}, err
				}
			}
		}
		if p.IsTask != nil {
			if err = repository.ToggleIsAsset(tx, p.Id, *p.IsTask); err != nil {
				return AssetResponse{}, err
			}
		}
		if p.AssetTypeId != nil {
			if err = repository.ChangeAssetType(tx, p.Id, *p.AssetTypeId); err != nil {
				return AssetResponse{}, err
			}
		}
		a, e := repository.GetSimpleAsset(tx, p.Id)
		if e != nil {
			return AssetResponse{}, e
		}
		a.Synced = true
		out.Assets = append(out.Assets, a)
	}
	out.SyncToken = utils.GenerateToken()
	err = utils.SetProjectSyncToken(tx, out.SyncToken)
	return out, err
}
func ApplyCollections(tx *sqlx.Tx, actorId string, req CollectionRequest) (CollectionResponse, error) {
	if len(req.Collections) == 0 {
		return CollectionResponse{}, fmt.Errorf("collections array is required")
	}
	actor, err := repository.GetUser(tx, actorId)
	if err != nil || !actor.Role.UpdateCollection {
		return CollectionResponse{}, ErrForbidden
	}
	for _, p := range req.Collections {
		var n int
		if p.Id == "" {
			return CollectionResponse{}, fmt.Errorf("collection id is required")
		}
		if err = tx.Get(&n, "SELECT COUNT(*) FROM collection WHERE id=? AND trashed=0", p.Id); err != nil || n == 0 {
			return CollectionResponse{}, fmt.Errorf("collection_not_found: %s", p.Id)
		}
		if p.CollectionTypeId != nil {
			if _, err = repository.GetCollectionType(tx, *p.CollectionTypeId); err != nil {
				return CollectionResponse{}, err
			}
		}
		for _, uid := range p.AddAssigneeIds {
			if _, err = repository.GetUser(tx, uid); err != nil {
				return CollectionResponse{}, fmt.Errorf("assignee_not_collaborator: %s", uid)
			}
		}
	}
	out := CollectionResponse{Collections: make([]models.Collection, 0, len(req.Collections))}
	for _, p := range req.Collections {
		if p.IsShared != nil {
			if err = repository.ChangeIsShared(tx, p.Id, *p.IsShared); err != nil {
				return CollectionResponse{}, err
			}
		}
		if p.CollectionTypeId != nil {
			if err = repository.ChangeCollectionType(tx, p.Id, *p.CollectionTypeId); err != nil {
				return CollectionResponse{}, err
			}
		}
		for _, uid := range p.AddAssigneeIds {
			var n int
			if err = tx.Get(&n, "SELECT COUNT(*) FROM collection_assignee WHERE collection_id=? AND assignee_id=?", p.Id, uid); err != nil {
				return CollectionResponse{}, err
			}
			if n == 0 {
				if err = repository.AssignCollection(tx, p.Id, uid); err != nil {
					return CollectionResponse{}, err
				}
				_, err = tx.Exec("UPDATE collection_assignee SET assigner_id=? WHERE collection_id=? AND assignee_id=?", actorId, p.Id, uid)
				if err != nil {
					return CollectionResponse{}, err
				}
			}
		}
		for _, uid := range p.RemoveAssigneeIds {
			if _, err = tx.Exec("DELETE FROM collection_assignee WHERE collection_id=? AND assignee_id=?", p.Id, uid); err != nil {
				return CollectionResponse{}, err
			}
		}
		var c models.Collection
		if err = tx.Get(&c, "SELECT * FROM collection WHERE id=?", p.Id); err != nil {
			return CollectionResponse{}, err
		}
		c.Synced = true
		out.Collections = append(out.Collections, c)
		var rows []models.CollectionAssignee
		if err = tx.Select(&rows, "SELECT * FROM collection_assignee WHERE collection_id=?", p.Id); err != nil {
			return CollectionResponse{}, err
		}
		out.CollectionAssignees = append(out.CollectionAssignees, rows...)
	}
	out.SyncToken = utils.GenerateToken()
	err = utils.SetProjectSyncToken(tx, out.SyncToken)
	return out, err
}

func PutAssetType(tx *sqlx.Tx, actorId string, req TypePutRequest) (AssetTypeResponse, error) {
	actor, err := repository.GetUser(tx, actorId)
	if err != nil || actor.Role.Name != "admin" {
		return AssetTypeResponse{}, ErrForbidden
	}
	if req.Id == "" || req.Name == "" {
		return AssetTypeResponse{}, fmt.Errorf("id and name are required")
	}
	assetType, err := repository.GetAssetType(tx, req.Id)
	if errors.Is(err, error_service.ErrAssetTypeNotFound) {
		assetType, err = repository.CreateAssetType(tx, req.Id, req.Name, req.Icon)
	} else if err == nil && (assetType.Name != req.Name || assetType.Icon != req.Icon) {
		assetType, err = repository.UpdateAssetType(tx, req.Id, req.Name, req.Icon)
	}
	if err != nil {
		return AssetTypeResponse{}, err
	}
	assetType.Synced = true
	out := AssetTypeResponse{AssetType: assetType, SyncToken: utils.GenerateToken()}
	err = utils.SetProjectSyncToken(tx, out.SyncToken)
	return out, err
}

func PutCollectionType(tx *sqlx.Tx, actorId string, req TypePutRequest) (CollectionTypeResponse, error) {
	actor, err := repository.GetUser(tx, actorId)
	if err != nil || actor.Role.Name != "admin" {
		return CollectionTypeResponse{}, ErrForbidden
	}
	if req.Id == "" || req.Name == "" {
		return CollectionTypeResponse{}, fmt.Errorf("id and name are required")
	}
	collectionType, err := repository.GetCollectionType(tx, req.Id)
	if errors.Is(err, error_service.ErrCollectionTypeNotFound) {
		collectionType, err = repository.CreateCollectionType(tx, req.Id, req.Name, req.Icon)
	} else if err == nil && (collectionType.Name != req.Name || collectionType.Icon != req.Icon) {
		collectionType, err = repository.UpdateCollectionType(tx, req.Id, req.Name, req.Icon)
	}
	if err != nil {
		return CollectionTypeResponse{}, err
	}
	collectionType.Synced = true
	out := CollectionTypeResponse{CollectionType: collectionType, SyncToken: utils.GenerateToken()}
	err = utils.SetProjectSyncToken(tx, out.SyncToken)
	return out, err
}
