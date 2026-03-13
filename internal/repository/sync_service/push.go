package sync_service

import (
	"bytes"
	"clustta/internal/chunk_service"
	"clustta/internal/constants"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"
	"clustta/internal/utils"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DataDog/zstd"
	"google.golang.org/protobuf/proto"
)

func PushData(projectPath, remoteUrl string, userId string, callback func(int, int, string, string)) error {
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

	// data, err := LoadCheckpointData(tx)
	err = repository.ClearTrash(tx)
	if err != nil {
		return err
	}

	data, err := LoadChangedData(tx)
	if err != nil {
		return err
	}
	if data.IsEmpty() {
		return nil
	}
	pdData := repositorypb.ProjectData{
		ProjectPreview:  data.ProjectPreview,
		CollectionTypes:     repository.ToPbCollectionTypes(data.CollectionTypes),
		Collections:        repository.ToPbCollections(data.Collections),
		CollectionAssignees: repository.ToPbCollectionAssignees(data.CollectionAssignees),

		AssetTypes:          repository.ToPbAssetTypes(data.AssetTypes),
		Assets:              repository.ToPbAssets(data.Assets),
		AssetsCheckpoints:   repository.ToPbCheckpoints(data.AssetsCheckpoints),
		AssetDependencies:   repository.ToPbAssetDependencies(data.AssetDependencies),
		CollectionDependencies: repository.ToPbCollectionDependencies(data.CollectionDependencies),

		Statuses:        repository.ToPbStatuses(data.Statuses),
		DependencyTypes: repository.ToPbDependencyTypes(data.DependencyTypes),

		Users: repository.ToPbUsers(data.Users),
		Roles: repository.ToPbRoles(data.Roles),

		Templates: repository.ToPbTemplates(data.Templates),

		Workflows:        repository.ToPbWorkflows(data.Workflows),
		WorkflowLinks:    repository.ToPbWorkflowLinks(data.WorkflowLinks),
		WorkflowCollections: repository.ToPbWorkflowCollections(data.WorkflowCollections),
		WorkflowAssets:    repository.ToPbWorkflowAssets(data.WorkflowAssets),

		Tags:      repository.ToPbTags(data.Tags),
		AssetsTags: repository.ToPbAssetTags(data.AssetsTags),

		Tomb: repository.ToPbTombs(data.Tombs),

		IntegrationProjects:           repository.ToPbIntegrationProjects(data.IntegrationProjects),
		IntegrationCollectionMappings: repository.ToPbIntegrationCollectionMappings(data.IntegrationCollectionMappings),
		IntegrationAssetMappings:      repository.ToPbIntegrationAssetMappings(data.IntegrationAssetMappings),
	}

	dataByte, err := proto.Marshal(&pdData)
	if err != nil {
		return err
	}

	compressedData, err := zstd.CompressLevel(nil, dataByte, 3)
	if err != nil {
		return err
	}

	chunks := []string{}
	for _, AssetCheckpoint := range data.AssetsCheckpoints {
		chunksString := AssetCheckpoint.Chunks
		chunkHashes := strings.Split(chunksString, ",")
		for _, chunkHash := range chunkHashes {
			if !utils.Contains(chunks, chunkHash) {
				chunks = append(chunks, chunkHash)
			}
		}

	}
	for _, Template := range data.Templates {
		chunksString := Template.Chunks
		chunkHashes := strings.Split(chunksString, ",")
		for _, chunkHash := range chunkHashes {
			if !utils.Contains(chunks, chunkHash) {
				chunks = append(chunks, chunkHash)
			}
		}

	}

	remoteMissingChunks, err := FetchMissingChunks(remoteUrl, userId, chunks)
	if err != nil {
		return err
	}
	if len(remoteMissingChunks) > 0 {
		remoteMissingChunksInfo, err := chunk_service.GetChunksInfo(tx, remoteMissingChunks)
		if err != nil {
			return err
		}
		err = chunk_service.PushChunksBatch(tx, remoteUrl, userId, remoteMissingChunksInfo, callback)
		if err != nil {
			return err
		}
	}

	previewIds := []string{}
	if data.ProjectPreview != "" && !utils.Contains(previewIds, data.ProjectPreview) {
		previewIds = append(previewIds, data.ProjectPreview)
	}
	for _, asset := range data.Assets {
		if asset.PreviewId != "" && !utils.Contains(previewIds, asset.PreviewId) {
			previewIds = append(previewIds, asset.PreviewId)
		}
	}
	for _, collection := range data.Collections {
		if collection.PreviewId != "" && !utils.Contains(previewIds, collection.PreviewId) {
			previewIds = append(previewIds, collection.PreviewId)
		}
	}
	for _, assetCheckpoint := range data.AssetsCheckpoints {
		if assetCheckpoint.PreviewId != "" && !utils.Contains(previewIds, assetCheckpoint.PreviewId) {
			previewIds = append(previewIds, assetCheckpoint.PreviewId)
		}
	}

	remoteMissingPreviews, err := FetchMissingPreviews(remoteUrl, userId, previewIds)
	if err != nil {
		return err
	}

	if len(remoteMissingPreviews) > 0 {
		err = repository.PushPreviews(tx, remoteUrl, userId, remoteMissingPreviews, callback)
		if err != nil {
			return err
		}
	}

	if utils.IsValidURL(remoteUrl) {
		dataUrl := remoteUrl + "/data"

		// jsonData, err := json.Marshal(data)
		// if err != nil {
		// 	return err
		// }

		req, err := http.NewRequest("POST", dataUrl, bytes.NewBuffer(compressedData))
		if err != nil {
			return err
		}
		req.Header.Set("Clustta-Agent", constants.USER_AGENT)

		client := &http.Client{
			Timeout: 10 * time.Minute, // total time including connection, redirects, reading body
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		defer response.Body.Close()

		responseCode := response.StatusCode
		if responseCode == 200 {
			err = utils.SetTablesToSynced(tx, ProjectTables)
			if err != nil {
				return err
			}
			err = tx.Commit()
			if err != nil {
				return err
			}
			return nil
		} else {
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err
			}
			return errors.New(string(body))
		}
	} else if utils.FileExists(remoteUrl) {
		db, err := utils.OpenDb(remoteUrl)
		if err != nil {
			return err
		}
		defer db.Close()
		remoteTx, err := db.Beginx()
		if err != nil {
			return err
		}
		err = WriteProjectData(remoteTx, data, true)
		if err != nil {
			return err
		}
		err = remoteTx.Commit()
		if err != nil {
			remoteTx.Rollback()
			return err
		}

		err = utils.SetTablesToSynced(tx, ProjectTables)
		if err != nil {
			return err
		}
		err = tx.Commit()
		if err != nil {
			return err
		}

		return nil
	} else {
		return fmt.Errorf("invalid url:%s", remoteUrl)
	}
}
