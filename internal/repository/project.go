package repository

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"clustta/internal/auth_service"
	"clustta/internal/chunk_service"
	"clustta/internal/constants"
	"clustta/internal/error_service"
	"clustta/internal/repository/migrations"
	"clustta/internal/repository/models"
	"clustta/internal/settings"
	"clustta/internal/utils"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed template_files/*
var templateFS embed.FS

//go:embed schema.sql
var ProjectSchema string

type ProjectInfo struct {
	Id               string   `json:"id"`
	SyncToken        string   `json:"sync_token"`
	PreviewId        string   `json:"preview_id"`
	Name             string   `json:"name"`
	Icon             string   `json:"icon"`
	Version          float64  `json:"version"`
	Uri              string   `json:"uri"`
	WorkingDirectory string   `json:"working_directory"`
	Remote           string   `json:"remote"`
	Valid            bool     `json:"valid"`
	Status           string   `json:"status"`
	HasRemote        bool     `json:"has_remote"`
	IsUnsynced       bool     `json:"is_unsynced"`
	IsDownloaded     bool     `json:"is_downloaded"`
	IsClosed         bool     `json:"is_closed"`
	IsOutdated       bool     `json:"is_outdated"`
	IgnoreList       []string `json:"ignore_list"`
}

type ProjectConfig struct {
	Name  string      `json:"name" db:"name"`
	Value interface{} `json:"value" db:"value"`
	Mtime int         `json:"mtime" db:"mtime"`
}

func InitDB(projectPath string, studioName, workingDir string, user auth_service.User, walMode bool) error {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer db.Close()

	if walMode {
		_, err = db.Exec("PRAGMA journal_mode = WAL;")
		if err != nil {
			return err
		}
	}

	if !settings.IsServer() && workingDir == "" {
		defaultWorkingDirRoot, err := settings.GetWorkingDirectory()
		if err != nil {
			return err
		}
		projectName := strings.TrimSuffix(filepath.Base(projectPath), filepath.Ext(projectPath))
		workingDir = filepath.Join(defaultWorkingDirRoot, studioName, projectName)
	}

	if !settings.IsServer() {
		if _, err := os.Stat(workingDir); os.IsNotExist(err) {
			err = os.MkdirAll(workingDir, os.ModePerm)
			if err != nil {
				return err
			}
		}
	}

	// _, err = db.Exec("PRAGMA auto_vacuum = FULL;")
	// if err != nil {
	// 	return err
	// }

	// _, err = db.Exec("PRAGMA page_size = 65536;")
	// if err != nil {
	// 	return err
	// }

	// statements := strings.Split(ProjectSchema, ";")

	err = utils.CreateSchema(db, ProjectSchema)
	if err != nil {
		return err
	}

	// _, err = db.Exec("VACUUM;")
	// if err != nil {
	// 	return err
	// }

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	//add project_id to config
	project_id := uuid.New().String()
	_, err = tx.Exec("INSERT INTO config (name, value, mtime) VALUES ('project_id', ?, ?)", project_id, utils.GetEpochTime())
	if err != nil {
		return err
	}

	// Store project display name from filename
	projectName := strings.TrimSuffix(filepath.Base(projectPath), filepath.Ext(projectPath))
	err = utils.SetProjectName(tx, projectName)
	if err != nil {
		return err
	}
	// if err != nil && err.Error() == "UNIQUE constraint failed: config.name" {
	// 	//do nothing
	// } else if err != nil {
	// 	tx.Rollback()
	// 	return err
	// }

	_, err = tx.Exec("INSERT INTO config (name, value, mtime) VALUES ('remote', ?, ?)", "", utils.GetEpochTime())
	if err != nil {
		return err
	}
	err = utils.SetStudioName(tx, studioName)
	if err != nil {
		return err
	}

	err = utils.SetProjectWorkingDir(tx, workingDir)
	if err != nil {
		return err
	}

	err = initData(tx)
	if err != nil {
		return err
	}

	role, err := GetRoleByName(tx, "admin")
	if err != nil {
		tx.Rollback()
		return err
	}
	_, err = AddKnownUser(tx, user.Id, user.Email, user.Username, user.FirstName, user.LastName, role.Id, []byte{}, true)
	if err != nil {
		return err
	}
	err = utils.SetProjectVersion(tx, migrations.LatestVersion)
	if err != nil {
		return err
	}
	tx.Commit()

	return nil
}

func initData(tx *sqlx.Tx) error {
	_, err := GetOrCreateStatus(tx, "todo", "todo", "#c0c0c0")
	if err != nil {
		return err
	}
	_, err = GetOrCreateStatus(tx, "ready", "ready", "#f6a000")
	if err != nil {
		return err
	}
	_, err = GetOrCreateStatus(tx, "work in progress", "wip", "#7696ee")
	if err != nil {
		return err
	}
	_, err = GetOrCreateStatus(tx, "waiting for approval", "wfa", "#986dd1")
	if err != nil {
		return err
	}
	_, err = GetOrCreateStatus(tx, "retake", "retake", "#dd0620")
	if err != nil {
		return err
	}
	_, err = GetOrCreateStatus(tx, "done", "done", "#51e064")
	if err != nil {
		return err
	}

	_, err = GetOrCreateDependencyType(tx, "waiting on")
	if err != nil {
		return err
	}
	_, err = GetOrCreateDependencyType(tx, "blocking")
	if err != nil {
		return err
	}
	_, err = GetOrCreateDependencyType(tx, "linked")
	if err != nil {
		return err
	}
	_, err = GetOrCreateDependencyType(tx, "working")
	if err != nil {
		return err
	}

	adminRoleAttributes := models.RoleAttributes{
		ViewCollection:   true,
		CreateCollection: true,
		UpdateCollection: true,
		DeleteCollection: true,

		ViewAsset:   true,
		CreateAsset: true,
		UpdateAsset: true,
		DeleteAsset: true,

		ViewTemplate:   true,
		CreateTemplate: true,
		UpdateTemplate: true,
		DeleteTemplate: true,

		ViewCheckpoint:   true,
		CreateCheckpoint: true,
		DeleteCheckpoint: true,

		PullChunk: true,

		AssignAsset:   true,
		UnassignAsset: true,

		AddUser:    true,
		RemoveUser: true,
		ChangeRole: true,

		ChangeStatus:   true,
		SetDoneAsset:   true,
		SetRetakeAsset: true,

		ViewDoneAsset: true,

		ManageDependencies: true,
	}
	productionManagerRoleAttributes := models.RoleAttributes{
		ViewCollection:   true,
		CreateCollection: true,
		UpdateCollection: true,
		DeleteCollection: false,

		ViewAsset:   true,
		CreateAsset: true,
		UpdateAsset: true,
		DeleteAsset: false,

		ViewTemplate:   true,
		CreateTemplate: true,
		UpdateTemplate: true,
		DeleteTemplate: false,

		ViewCheckpoint:   true,
		CreateCheckpoint: false,
		DeleteCheckpoint: false,

		PullChunk: false,

		AssignAsset:   true,
		UnassignAsset: true,

		AddUser:    false,
		RemoveUser: false,
		ChangeRole: false,

		ChangeStatus:   true,
		SetDoneAsset:   true,
		SetRetakeAsset: true,

		ViewDoneAsset: true,

		ManageDependencies: true,
	}
	supervisorRoleAttributes := models.RoleAttributes{
		ViewCollection:   true,
		CreateCollection: false,
		UpdateCollection: false,
		DeleteCollection: false,

		ViewAsset:   true,
		CreateAsset: false,
		UpdateAsset: false,
		DeleteAsset: false,

		ViewTemplate:   false,
		CreateTemplate: false,
		UpdateTemplate: false,
		DeleteTemplate: false,

		ViewCheckpoint:   true,
		CreateCheckpoint: true,
		DeleteCheckpoint: true,

		PullChunk: true,

		AssignAsset:   true,
		UnassignAsset: true,

		AddUser:    false,
		RemoveUser: false,
		ChangeRole: false,

		ChangeStatus:   true,
		SetDoneAsset:   true,
		SetRetakeAsset: true,

		ViewDoneAsset: true,

		ManageDependencies: false,
	}
	assistantSupervisorRoleAttributes := models.RoleAttributes{
		ViewCollection:   false,
		CreateCollection: false,
		UpdateCollection: false,
		DeleteCollection: false,

		ViewAsset:   true,
		CreateAsset: false,
		UpdateAsset: false,
		DeleteAsset: false,

		ViewTemplate:   false,
		CreateTemplate: false,
		UpdateTemplate: false,
		DeleteTemplate: false,

		ViewCheckpoint:   true,
		CreateCheckpoint: true,
		DeleteCheckpoint: false,

		PullChunk: true,

		AssignAsset:   false,
		UnassignAsset: false,

		AddUser:    false,
		RemoveUser: false,
		ChangeRole: false,

		ChangeStatus:   true,
		SetDoneAsset:   true,
		SetRetakeAsset: true,

		ViewDoneAsset: true,

		ManageDependencies: false,
	}
	artistRoleAttributes := models.RoleAttributes{
		ViewCollection:   false,
		CreateCollection: false,
		UpdateCollection: false,
		DeleteCollection: false,

		ViewAsset:   false,
		CreateAsset: false,
		UpdateAsset: false,
		DeleteAsset: false,

		ViewTemplate:   false,
		CreateTemplate: false,
		UpdateTemplate: false,
		DeleteTemplate: false,

		ViewCheckpoint:   true,
		CreateCheckpoint: true,
		DeleteCheckpoint: false,

		PullChunk: true,

		AssignAsset:   false,
		UnassignAsset: false,

		AddUser:    false,
		RemoveUser: false,
		ChangeRole: false,

		ChangeStatus:   true,
		SetDoneAsset:   false,
		SetRetakeAsset: false,

		ViewDoneAsset: false,

		ManageDependencies: false,
	}
	vendorRoleAttributes := models.RoleAttributes{
		ViewCollection:   false,
		CreateCollection: false,
		UpdateCollection: false,
		DeleteCollection: false,

		ViewAsset:   false,
		CreateAsset: false,
		UpdateAsset: false,
		DeleteAsset: false,

		ViewTemplate:   false,
		CreateTemplate: false,
		UpdateTemplate: false,
		DeleteTemplate: false,

		ViewCheckpoint:   true,
		CreateCheckpoint: false,
		DeleteCheckpoint: false,

		PullChunk: true,

		AssignAsset:   false,
		UnassignAsset: false,

		AddUser:    false,
		RemoveUser: false,
		ChangeRole: false,

		ChangeStatus:   true,
		SetDoneAsset:   false,
		SetRetakeAsset: false,

		ViewDoneAsset: false,

		ManageDependencies: false,
	}
	_, err = GetOrCreateRole(tx, "admin", adminRoleAttributes)
	if err != nil {
		return err
	}
	_, err = GetOrCreateRole(tx, "production manager", productionManagerRoleAttributes)
	if err != nil {
		return err
	}
	_, err = GetOrCreateRole(tx, "supervisor", supervisorRoleAttributes)
	if err != nil {
		return err
	}
	_, err = GetOrCreateRole(tx, "assistant supervisor", assistantSupervisorRoleAttributes)
	if err != nil {
		return err
	}
	_, err = GetOrCreateRole(tx, "artist", artistRoleAttributes)
	if err != nil {
		return err
	}
	_, err = GetOrCreateRole(tx, "vendor", vendorRoleAttributes)
	if err != nil {
		return err
	}

	_, err = GetOrCreateAssetType(tx, "generic", "generic")
	if err != nil {
		return err
	}
	_, err = GetOrCreateCollectionType(tx, "generic", "folder")
	if err != nil {
		return err
	}
	return nil
}

func ClearTrash(tx *sqlx.Tx) error {
	deleteAssetAndCollections := `
		-- Delete asset_checkpoint records
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM asset_checkpoint 
		WHERE trashed = 1 
		OR asset_id IN (
			SELECT id FROM asset 
			WHERE trashed = 1 
			OR collection_id IN (SELECT id FROM trashed_collections)
		);

		-- Delete asset dependencies
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM asset_dependency 
		WHERE asset_id IN (
			SELECT id FROM asset 
			WHERE trashed = 1 
			OR collection_id IN (SELECT id FROM trashed_collections)
		)
		OR dependency_id IN (
			SELECT id FROM asset 
			WHERE trashed = 1 
			OR collection_id IN (SELECT id FROM trashed_collections)
		);

		-- Delete collection dependencies
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM collection_dependency 
		WHERE asset_id IN (
			SELECT id FROM asset 
			WHERE trashed = 1 
			OR collection_id IN (SELECT id FROM trashed_collections)
		)
		OR dependency_id IN (SELECT id FROM trashed_collections);

		-- Delete asset tags
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM asset_tag 
		WHERE asset_id IN (
			SELECT id FROM asset 
			WHERE trashed = 1 
			OR collection_id IN (SELECT id FROM trashed_collections)
		);

		-- Delete assets
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM asset 
		WHERE trashed = 1 
		OR collection_id IN (SELECT id FROM trashed_collections);

		-- Delete templates
		DELETE FROM template WHERE trashed = 1;

		-- Delete collections
		WITH RECURSIVE trashed_collections AS (
			SELECT id FROM collection WHERE trashed = 1
			UNION
			SELECT e.id FROM collection e
			INNER JOIN trashed_collections te ON e.parent_id = te.id
		)
		DELETE FROM collection WHERE id IN (SELECT id FROM trashed_collections);

		-- Clean up hanging references
		DELETE FROM asset WHERE collection_id != '' AND collection_id NOT IN (SELECT id FROM collection);
		DELETE FROM asset_checkpoint WHERE asset_id NOT IN (SELECT id FROM asset);
		DELETE FROM asset_dependency WHERE asset_id NOT IN (SELECT id FROM asset) OR dependency_id NOT IN (SELECT id FROM asset);
		DELETE FROM collection_dependency WHERE asset_id NOT IN (SELECT id FROM asset) OR dependency_id NOT IN (SELECT id FROM collection);
		DELETE FROM asset_tag WHERE asset_id NOT IN (SELECT id FROM asset) OR tag_id NOT IN (SELECT id FROM tag);
	`

	_, err := tx.Exec(deleteAssetAndCollections)
	if err != nil {
		return err
	}

	return nil
}

func Purge(projectPath string) error {
	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	err = ClearTrash(tx)
	if err != nil {
		return err
	}

	clearUnusedChunks := `
	WITH used_chunks AS (
		-- Chunks used in templates
		SELECT DISTINCT TRIM(value) as hash
		FROM template, json_each('["' || REPLACE(chunks, ',', '","') || '"]')
		WHERE chunks != ''
		UNION
		-- Chunks used in asset_checkpoints
		SELECT DISTINCT TRIM(value) as hash
		FROM asset_checkpoint, json_each('["' || REPLACE(chunks, ',', '","') || '"]')
		WHERE chunks != ''
	)
	DELETE FROM chunk 
	WHERE hash NOT IN (SELECT hash FROM used_chunks);
	`

	_, err = tx.Exec(clearUnusedChunks)
	if err != nil {
		tx.Rollback()
		return err
	}

	tx.Commit()

	// _, err = dbConn.Exec("PRAGMA incremental_vacuum(100);")
	// if err != nil {
	// 	return err
	// }
	// _, err = dbConn.Exec("VACUUM")
	// if err != nil {
	// 	return err
	// }

	return nil
}

func Vacuum(projectPath string) error {
	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}

	_, err = dbConn.Exec("VACUUM")
	if err != nil {
		return err
	}

	return nil
}

func ClearProjectOrphans(projectPath string) error {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		CREATE TEMPORARY TABLE IF NOT EXISTS temp_orphan_collections (id TEXT PRIMARY KEY);
		CREATE TEMPORARY TABLE IF NOT EXISTS temp_orphan_assets (id TEXT PRIMARY KEY);

		DELETE FROM temp_orphan_collections;
		DELETE FROM temp_orphan_assets;

		INSERT OR REPLACE INTO temp_orphan_collections
		WITH RECURSIVE orphan_collections AS (
			-- Base case: collections with non-empty parent_id that doesn't exist in collection table
			SELECT DISTINCT id
			FROM collection
			WHERE parent_id != '' 
			AND NOT EXISTS (SELECT 1 FROM collection parent WHERE parent.id = collection.parent_id)
			
			UNION
			
			-- Recursive case: collections whose parent is an orphan
			SELECT DISTINCT e.id
			FROM collection e
			JOIN orphan_collections oe ON e.parent_id = oe.id
		)
		SELECT id FROM orphan_collections;

		-- Find orphan assets and store in temp table
		INSERT OR REPLACE INTO temp_orphan_assets
		SELECT DISTINCT id
		FROM asset
		WHERE 
			-- Assets with non-empty collection_id that doesn't exist
			(collection_id != '' AND NOT EXISTS (SELECT 1 FROM collection e WHERE e.id = collection_id))
			-- Or assets whose collection is an orphan
			OR (collection_id IN (SELECT id FROM temp_orphan_collections));

		-- Delete asset_checkpoint records related to orphan assets
		DELETE FROM asset_checkpoint
		WHERE asset_id IN (SELECT id FROM temp_orphan_assets);

		-- Delete asset_tag records related to orphan assets
		DELETE FROM asset_tag
		WHERE asset_id IN (SELECT id FROM temp_orphan_assets);

		-- Delete asset_dependency records where either asset is an orphan
		DELETE FROM asset_dependency
		WHERE asset_id IN (SELECT id FROM temp_orphan_assets)
		OR dependency_id IN (SELECT id FROM temp_orphan_assets);

		-- Delete collection_dependency records related to orphan assets or collections
		DELETE FROM collection_dependency
		WHERE asset_id IN (SELECT id FROM temp_orphan_assets)
		OR dependency_id IN (SELECT id FROM temp_orphan_collections);

		-- Now delete the orphan assets
		DELETE FROM asset
		WHERE id IN (SELECT id FROM temp_orphan_assets);

		-- Delete orphan collections
		DELETE FROM collection
		WHERE id IN (SELECT id FROM temp_orphan_collections);

		-- Clean up temporary tables
		DROP TABLE IF EXISTS temp_orphan_collections;
		DROP TABLE IF EXISTS temp_orphan_assets;
	`
	_, err = tx.Exec(query)
	if err != nil {
		return err
	}

	return nil
}

func VerifyProjectIntegrity(projectPath string) (bool, error) {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	// tx, err := db.Beginx()
	// if err != nil {
	// 	return false, err
	// }
	// err = initData(tx)
	// if err != nil {
	// 	tx.Rollback()
	// 	return false, err
	// }
	// tx.Commit()
	tableNames := []string{
		"config", "template", "tag", "status",
		"collection", "collection_type", "asset", "asset_type",
		"dependency_type", "asset_dependency", "asset_tag",
		"asset_checkpoint", "chunk",
		"user",
	}
	for _, tableName := range tableNames {
		rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tableName)
		if err != nil {
			return false, err
		}
		defer rows.Close()
		if !rows.Next() {
			return false, nil
		}
	}

	tx, err := db.Beginx()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	statuses, err := GetStatuses(tx)
	if err != nil {
		return false, err
	}
	if len(statuses) < 3 {
		return false, errors.New("clst file missing data")
	}

	return true, nil
}

func AutoGroupCheckpoints(tx *sqlx.Tx) error {
	type GroupedTimeline struct {
		CreatedAt     string   `db:"created_at" json:"created_at"`
		CheckpointIds []string `db:"checkpoint_ids" json:"checkpoint_ids"`
		Comment       string   `db:"comment" json:"comment"`
		AuthorUID     string   `db:"author_id" json:"author_id"`
		GroupId       string
	}
	type MiniCheckpoint struct {
		Id        string `db:"id" json:"id"`
		CreatedAt string `db:"created_at" json:"created_at"`
		Comment   string `db:"comment" json:"comment"`
		AuthorUID string `db:"author_id" json:"author_id"`
	}
	timeline := []GroupedTimeline{}
	checkpoints := []MiniCheckpoint{}
	query := `SELECT 
		asset_checkpoint.id,
		asset_checkpoint.created_at,
		asset_checkpoint.comment,
		asset_checkpoint.author_id
	FROM 
		asset_checkpoint
	ORDER BY asset_checkpoint.created_at DESC;`
	err := tx.Select(&checkpoints, query)
	if err != nil && err == sql.ErrNoRows {
		return errors.New("no checkpoints")
	} else if err != nil {
		return err
	}

	previousCheckpoint := GroupedTimeline{}
	for i, checkpoint := range checkpoints {
		if previousCheckpoint.CreatedAt == "" {
			previousCheckpoint = GroupedTimeline{
				CreatedAt:     checkpoint.CreatedAt,
				CheckpointIds: []string{checkpoint.Id},
				Comment:       checkpoint.Comment,
				AuthorUID:     checkpoint.AuthorUID,
				GroupId:       uuid.New().String(),
			}
			if i == len(checkpoints)-1 {
				timeline = append(timeline, previousCheckpoint)
			}
			continue
		}
		checkpointTime, err := time.Parse(time.RFC3339, checkpoint.CreatedAt)
		if err != nil {
			return err
		}
		prevCheckpointTime, err := time.Parse(time.RFC3339, previousCheckpoint.CreatedAt)
		if err != nil {
			return err
		}

		// Calculate the difference
		diff := prevCheckpointTime.Sub(checkpointTime)
		if previousCheckpoint.AuthorUID == checkpoint.AuthorUID && previousCheckpoint.Comment == checkpoint.Comment && diff.Seconds() <= 120 {
			previousCheckpoint.CheckpointIds = append(previousCheckpoint.CheckpointIds, checkpoint.Id)
		} else {
			timeline = append(timeline, previousCheckpoint)
			previousCheckpoint = GroupedTimeline{
				CreatedAt:     checkpoint.CreatedAt,
				CheckpointIds: []string{checkpoint.Id},
				Comment:       checkpoint.Comment,
				AuthorUID:     checkpoint.AuthorUID,
				GroupId:       uuid.New().String(),
			}
		}
		if i == len(checkpoints)-1 {
			timeline = append(timeline, previousCheckpoint)
		}
	}

	for _, group := range timeline {
		for _, checkpointId := range group.CheckpointIds {
			_, err := tx.Exec("UPDATE asset_checkpoint SET group_id = ? WHERE id = ?", group.GroupId, checkpointId)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func UpdateProject(projectPath string) error {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		return err
	}

	projectVersion, err := utils.GetProjectVersion(tx)
	if err != nil {
		tx.Rollback()
		return err
	}
	tx.Rollback()

	return migrations.RunMigrations(db, projectVersion, ProjectSchema)
}

func CreateProject(projectUri, studioName, workingDir, templateName string, user auth_service.User) (ProjectInfo, error) {
	projectInfo := ProjectInfo{}
	if templateName == "" {
		templateName = "No Template"
	}

	// userDataDir, err := settings.GetUserDataFolder()
	// if err != nil {
	// 	return projectInfo, err
	// }

	if utils.IsValidURL(projectUri) {
		req, err := http.NewRequest("POST", projectUri, nil)
		if err != nil {
			return projectInfo, err
		}
		userJson, err := json.Marshal(user)
		if err != nil {
			return projectInfo, err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return projectInfo, err
		}
		defer response.Body.Close()

		responseCode := response.StatusCode
		if responseCode == 200 {

			body, err := io.ReadAll(response.Body)
			if err != nil {
				return ProjectInfo{}, err
			}

			err = json.Unmarshal(body, &projectInfo)
			if err != nil {
				return projectInfo, err
			}

			projectInfo.HasRemote = false
			projectInfo.Uri = projectUri
			projectInfo.Remote = projectUri
			// projectInfo.WorkingData = workingData
			// projectInfo.WorkingDirectory = workingDir
			return projectInfo, nil
		} else if responseCode == 400 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return projectInfo, err
			}
			return projectInfo, errors.New(string(body))
		}
	} else {
		projectDir := filepath.Dir(projectUri)
		os.MkdirAll(projectDir, os.ModePerm)
		if utils.FileExists(projectUri) {
			verify, err := VerifyProjectIntegrity(projectUri)
			if err != nil {
				if err.Error() == "file is not a database" {
					return ProjectInfo{}, error_service.ErrInvalidProjectExists
				}
				return ProjectInfo{}, err
			}
			if verify {
				return ProjectInfo{}, error_service.ErrProjectExists
			}
			return ProjectInfo{}, error_service.ErrInvalidProjectExists
		}
		err := InitDB(projectUri, studioName, workingDir, user, false)
		if err != nil {
			return ProjectInfo{}, err
		}

		if templateName != "No Template" {
			ProjectTemplatesPath, err := settings.GetUserProjectTemplatesPath()
			if err != nil {
				return ProjectInfo{}, err
			}
			templatePath := filepath.Join(ProjectTemplatesPath, templateName+".clst")

			err = LoadProjectTemplateData(projectUri, templatePath)
			if err != nil {
				return ProjectInfo{}, err
			}
		}

		projectInfo, err := GetProjectInfo(projectUri, user)
		if err != nil {
			return ProjectInfo{}, err
		}

		projectInfo.HasRemote = false
		projectInfo.Uri = projectUri
		projectInfo.Remote = projectUri
		projectInfo.IsDownloaded = true
		return projectInfo, nil
	}
	return ProjectInfo{}, errors.New("invalid uri")
}

func GetProjectInfo(projectUri string, user auth_service.User) (ProjectInfo, error) {
	// userDataDir, err := settings.GetUserDataFolder()
	// if err != nil {
	// 	return ProjectInfo{}, err
	// }

	// var projectName string
	// hasRemote := false
	// if utils.IsValidURL(projectUri) {
	// 	projectName = path.Base(projectUri)
	// 	hasRemote = true
	// } else if utils.FileExists(projectUri) {
	// 	projectName = strings.Split(filepath.Base(projectUri), ".")[0]
	// }
	if utils.IsValidURL(projectUri) {
		projectUrl := projectUri
		req, err := http.NewRequest("GET", projectUrl, nil)
		if err != nil {
			return ProjectInfo{}, err
		}
		userJson, err := json.Marshal(user)
		if err != nil {
			return ProjectInfo{}, err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return ProjectInfo{}, err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return ProjectInfo{}, err
			}
			return ProjectInfo{}, errors.New(string(body))
		}

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return ProjectInfo{}, err
		}
		projectInfo := ProjectInfo{}
		err = json.Unmarshal(body, &projectInfo)
		if err != nil {
			return projectInfo, err
		}
		return projectInfo, nil
	} else if utils.FileExists(projectUri) {
		absProjectPath, err := utils.ExpandPath(projectUri)
		if err != nil {
			return ProjectInfo{}, err
		}
		if !utils.FileExists(absProjectPath) {
			return ProjectInfo{}, error_service.ErrProjectNotFound
		}
		db, err := utils.OpenDb(absProjectPath)
		if err != nil {
			return ProjectInfo{}, err
		}
		defer db.Close()
		tx, err := db.Beginx()
		if err != nil {
			return ProjectInfo{}, err
		}
		defer tx.Rollback()

		projectName, err := utils.GetProjectName(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		projectVersion, err := utils.GetProjectVersion(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		isClosed, err := utils.GetIsClosed(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		workingDir, err := utils.GetProjectWorkingDir(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		projectId, err := utils.GetProjectId(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		projectPreview, err := GetProjectPreview(tx)
		if err != nil && err.Error() != "no preview" {
			return ProjectInfo{}, err
		}
		icon, err := utils.GetProjectIcon(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		ignoreList, err := GetIgnoreList(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		syncToken, err := utils.GetProjectSyncToken(tx)
		if err != nil {
			return ProjectInfo{}, err
		}
		return ProjectInfo{
			Id:               projectId,
			SyncToken:        syncToken,
			PreviewId:        projectPreview.Hash,
			Name:             projectName,
			Icon:             icon,
			Version:          projectVersion,
			Remote:           "",
			Uri:              absProjectPath,
			WorkingDirectory: workingDir,
			Status:           "normal",
			HasRemote:        false,
			IsClosed:         isClosed,
			IgnoreList:       ignoreList,
		}, nil
	} else {
		return ProjectInfo{}, fmt.Errorf("invalid url:%s", projectUri)
	}
}

func GetSyncToken(projectUri string, user auth_service.User) (string, error) {
	if utils.IsValidURL(projectUri) {
		projectUrl := projectUri + "/sync-token"
		req, err := http.NewRequest("GET", projectUrl, nil)
		if err != nil {
			return "", err
		}
		userJson, err := json.Marshal(user)
		if err != nil {
			return "", err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return "", err
			}
			return "", errors.New(string(body))
		}

		body, err := io.ReadAll(response.Body)
		if err != nil {
			return "", err
		}

		return string(body), nil
	} else if utils.FileExists(projectUri) {
		absProjectPath, err := utils.ExpandPath(projectUri)
		if err != nil {
			return "", err
		}
		if !utils.FileExists(absProjectPath) {
			return "", error_service.ErrProjectNotFound
		}
		db, err := utils.OpenDb(absProjectPath)
		if err != nil {
			return "", err
		}
		defer db.Close()
		tx, err := db.Beginx()
		if err != nil {
			return "", err
		}
		defer tx.Rollback()

		syncToken, err := utils.GetProjectSyncToken(tx)
		if err != nil {
			return "", err
		}
		return syncToken, nil
	} else {
		return "", fmt.Errorf("invalid url:%s", projectUri)
	}
}

func UserInProject(projectPath string, userId string) (bool, error) {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return false, err
	}
	defer db.Close()
	tx, err := db.Beginx()
	defer tx.Rollback()
	if err != nil {
		return false, err
	}
	_, err = GetUser(tx, userId)
	if err != nil {
		if errors.Is(err, error_service.ErrUserNotFound) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

func RenameProject(projectUri, studioName, newName string, user auth_service.User) error {
	if utils.IsValidURL(projectUri) {
		data := []byte(fmt.Sprintf(`{"name": "%s"}`, newName))

		req, err := http.NewRequest(http.MethodPut, projectUri, bytes.NewBuffer(data))
		if err != nil {
			return err
		}

		userJson, err := json.Marshal(user)
		if err != nil {
			return err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err
			}
			return errors.New(string(body))
		}

		sharedProjectsDir, err := settings.GetSharedProjectDirectory()
		if err != nil {
			return err
		}
		studioProjectsDir := filepath.Join(sharedProjectsDir, studioName)

		paths := strings.Split(projectUri, "/")

		oldProjectName := paths[len(paths)-1]
		oldProjectPath := filepath.Join(studioProjectsDir, oldProjectName+".clst")
		newProjectPath := filepath.Join(studioProjectsDir, newName+".clst")
		err = os.Rename(oldProjectPath, newProjectPath)
		if err != nil {
			return err
		}

		return nil
	} else {
		newProjectPath := filepath.Join(filepath.Dir(projectUri), newName+".clst")
		err := os.Rename(projectUri, newProjectPath)
		if err != nil {
			return err
		}

		// Update project_name in config table
		dbConn, err := utils.OpenDb(newProjectPath)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		err = utils.SetProjectName(tx, newName)
		if err != nil {
			return err
		}
		return tx.Commit()
	}

}

func SetIcon(projectUri, studioName, icon string, user auth_service.User) error {
	if utils.IsValidURL(projectUri) {
		data := []byte(fmt.Sprintf(`{"icon": "%s"}`, icon))
		url := projectUri + "/icon"
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(data))
		if err != nil {
			return err
		}

		userJson, err := json.Marshal(user)
		if err != nil {
			return err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err
			}
			return errors.New(string(body))
		}

		sharedProjectsDir, err := settings.GetSharedProjectDirectory()
		if err != nil {
			return err
		}
		studioProjectsDir := filepath.Join(sharedProjectsDir, studioName)

		paths := strings.Split(projectUri, "/")

		projectName := paths[len(paths)-1]
		projectPath := filepath.Join(studioProjectsDir, projectName+".clst")

		dbConn, err := utils.OpenDb(projectPath)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		err = utils.SetProjectIcon(tx, icon)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}
		return nil
	} else {
		dbConn, err := utils.OpenDb(projectUri)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		err = utils.SetProjectIcon(tx, icon)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}
		return nil
	}

}

func ToggleCloseProject(projectUri, studioName string, user auth_service.User) error {
	if utils.IsValidURL(projectUri) {
		url := projectUri + "/toggle-close"
		req, err := http.NewRequest(http.MethodPut, url, nil)
		if err != nil {
			return err
		}

		userJson, err := json.Marshal(user)
		if err != nil {
			return err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err
			}
			return errors.New(string(body))
		}

		sharedProjectsDir, err := settings.GetSharedProjectDirectory()
		if err != nil {
			return err
		}
		studioProjectsDir := filepath.Join(sharedProjectsDir, studioName)

		paths := strings.Split(projectUri, "/")
		projectName := paths[len(paths)-1]
		projectPath := filepath.Join(studioProjectsDir, projectName+".clst")

		if utils.FileExists(projectPath) {
			dbConn, err := utils.OpenDb(projectPath)
			if err != nil {
				return err
			}
			defer dbConn.Close()
			tx, err := dbConn.Beginx()
			if err != nil {
				return err
			}
			defer tx.Rollback()

			isClosed, err := utils.GetIsClosed(tx)
			if err != nil {
				return err
			}

			err = utils.SetIsClosed(tx, !isClosed)
			if err != nil {
				return err
			}
			err = tx.Commit()
			if err != nil {
				return err
			}
		}

		return nil
	} else {
		dbConn, err := utils.OpenDb(projectUri)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		isClosed, err := utils.GetIsClosed(tx)
		if err != nil {
			return err
		}

		err = utils.SetIsClosed(tx, !isClosed)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}
		return nil
	}
}

func SetProjectPreview(tx *sqlx.Tx, previewPath string) error {
	preview, err := CreatePreview(tx, previewPath)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO config (name, value, mtime, synced)
		VALUES ('project_preview', $1, $2, 0)
		ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, mtime = EXCLUDED.mtime, synced = 0
	`, preview.Hash, utils.GetEpochTime())
	if err != nil {
		return err
	}
	return nil
}

func GetProjectPreview(tx *sqlx.Tx) (models.Preview, error) {
	var previewHash string
	err := tx.Get(&previewHash, `
        SELECT value 
        FROM config 
        WHERE name = 'project_preview'
    `)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Preview{}, errors.New("no preview")
		}
		return models.Preview{}, err
	}
	preview, err := GetPreview(tx, previewHash)
	if err != nil {
		return models.Preview{}, err
	}
	return preview, nil
}

func SetIgnoreList(projectUri, studioName string, ignoreList []string, user auth_service.User) error {
	if utils.IsValidURL(projectUri) {
		ignoreListJson, err := json.Marshal(ignoreList)
		if err != nil {
			return err
		}
		url := projectUri + "/ignore-list"
		req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(ignoreListJson))
		if err != nil {
			return err
		}

		userJson, err := json.Marshal(user)
		if err != nil {
			return err
		}
		req.Header.Set("UserData", string(userJson))
		req.Header.Set("UserId", user.Id)
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err
			}
			return errors.New(string(body))
		}

		sharedProjectsDir, err := settings.GetSharedProjectDirectory()
		if err != nil {
			return err
		}
		studioProjectsDir := filepath.Join(sharedProjectsDir, studioName)

		paths := strings.Split(projectUri, "/")

		projectName := paths[len(paths)-1]
		projectPath := filepath.Join(studioProjectsDir, projectName+".clst")

		dbConn, err := utils.OpenDb(projectPath)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		err = utils.SetProjectIgnoreList(tx, ignoreList)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}
		return nil
	} else {
		dbConn, err := utils.OpenDb(projectUri)
		if err != nil {
			return err
		}
		defer dbConn.Close()
		tx, err := dbConn.Beginx()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		err = utils.SetProjectIgnoreList(tx, ignoreList)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}
		return nil
	}
}

func GetIgnoreList(tx *sqlx.Tx) ([]string, error) {
	var ignoreListJson string
	err := tx.Get(&ignoreListJson, `
		SELECT value 
		FROM config 
		WHERE name = 'ignore_list'
	`)
	if err != nil {
		if err == sql.ErrNoRows {
			return []string{}, nil
		}
		return []string{}, err
	}
	var ignoreList []string
	err = json.Unmarshal([]byte(ignoreListJson), &ignoreList)
	if err != nil {
		return []string{}, err
	}
	return ignoreList, nil
}

func IsProjectPreviewSynced(tx *sqlx.Tx) (bool, error) {
	var isSynced bool
	err := tx.Get(&isSynced, `
        SELECT synced 
        FROM config 
        WHERE name = 'project_preview'
    `)
	if err != nil {
		if err == sql.ErrNoRows {
			return true, nil
		}
		return false, err
	}
	return isSynced, nil
}

func SetProjectPreviewSynced(tx *sqlx.Tx) error {
	_, err := tx.Exec(`
        UPDATE SET synced = 1
        FROM config 
        WHERE name = 'project_preview'
    `)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	return nil
}

func LoadProjectTemplateData(projectPath, templatePath string) error {
	templateDbConn, err := utils.OpenDb(templatePath)
	if err != nil {
		return err
	}
	defer templateDbConn.Close()
	templateTx, err := templateDbConn.Beginx()
	if err != nil {
		return err
	}
	defer templateTx.Rollback()

	templateAssetTypes, err := GetAssetTypes(templateTx)
	if err != nil {
		return err
	}
	templateCollectionTypes, err := GetCollectionTypes(templateTx)
	if err != nil {
		return err
	}
	templateIgnoreList, err := GetIgnoreList(templateTx)
	if err != nil {
		return err
	}

	templateAssetTemplates, err := GetTemplates(templateTx, false)
	if err != nil {
		return err
	}

	projectDbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer projectDbConn.Close()
	projectTx, err := projectDbConn.Beginx()
	if err != nil {
		return err
	}
	defer projectTx.Rollback()

	err = utils.SetProjectIgnoreList(projectTx, templateIgnoreList)
	if err != nil {
		return err
	}

	for _, templateAssetType := range templateAssetTypes {
		_, err = GetOrCreateAssetType(projectTx, templateAssetType.Name, templateAssetType.Icon)
		if err != nil {
			if err.Error() == "UNIQUE constraint failed: asset_type.icon" {
				continue
			}
			return err
		}
	}
	for _, templateCollectionType := range templateCollectionTypes {
		_, err = GetOrCreateCollectionType(projectTx, templateCollectionType.Name, templateCollectionType.Icon)
		if err != nil {
			if err.Error() == "UNIQUE constraint failed: collection_type.icon" {
				continue
			}
			return err
		}
	}

	chunks := []string{}
	for _, assetTemplate := range templateAssetTemplates {
		chunkHashes := strings.Split(assetTemplate.Chunks, ",")
		for _, chunkHash := range chunkHashes {
			if !utils.Contains(chunks, chunkHash) {
				chunks = append(chunks, chunkHash)
			}
		}
	}

	missingChunks, err := chunk_service.GetNonExistingChunks(projectTx, chunks)
	if err != nil {
		return fmt.Errorf("failed to get missing chunks: %w", err)
	}

	ChunksInfo, err := chunk_service.GetChunksInfo(templateTx, chunks)
	if err != nil {
		return err
	}

	err = projectTx.Commit()
	if err != nil {
		return err
	}
	err = templateTx.Rollback()
	if err != nil {
		return err
	}

	if len(missingChunks) > 0 {
		err = chunk_service.PullChunks(context.TODO(), projectPath, templatePath, ChunksInfo, func(i1, i2 int, s1, s2 string) {})
		if err != nil {
			return err
		}
	}

	projectTx, err = projectDbConn.Beginx()
	if err != nil {
		return err
	}
	defer projectTx.Rollback()

	for _, templateAssetTemplate := range templateAssetTemplates {
		_, err := GetTemplateByName(projectTx, templateAssetTemplate.Name)
		if err == nil {
			continue
		}
		_, err = AddTemplate(projectTx, templateAssetTemplate.Id, templateAssetTemplate.Name, templateAssetTemplate.Extension, templateAssetTemplate.Chunks, templateAssetTemplate.XxhashChecksum, templateAssetTemplate.FileSize)
		if err != nil {
			return err
		}

	}

	err = projectTx.Commit()
	if err != nil {
		return err
	}

	return nil
}
