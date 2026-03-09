package main

import (
	"bytes"
	"clustta/internal/chunk_service"
	"clustta/internal/constants"
	"clustta/internal/utils"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// CreateShareLinkHandler proxies the share link creation request to the global server.
// The studio server forwards the request with its credentials so the global server can
// store the share_link record in ClusttaDB.
func CreateShareLinkHandler(w http.ResponseWriter, r *http.Request) {
	if CONFIG.Private {
		SendErrorResponse(w, "File sharing is not available for private studios", http.StatusForbidden)
		return
	}

	userData := sessionManager.Get(r.Context(), "user")
	if userData == nil {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var requestingUser UserInfo
	if userBytes, ok := userData.([]byte); ok {
		if err := json.Unmarshal(userBytes, &requestingUser); err != nil {
			SendErrorResponse(w, "Invalid session data", http.StatusInternalServerError)
			return
		}
	} else {
		SendErrorResponse(w, "Invalid session data type", http.StatusInternalServerError)
		return
	}

	project := r.PathValue("project")

	var requestData struct {
		CheckpointIDs []string `json:"checkpoint_ids"`
		Label         string   `json:"label"`
		ExpiresIn     int      `json:"expires_in_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&requestData); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(requestData.CheckpointIDs) == 0 || requestData.Label == "" {
		SendErrorResponse(w, "checkpoint_ids and label are required", http.StatusBadRequest)
		return
	}

	// Forward to global server with studio credentials
	globalPayload := map[string]interface{}{
		"studio_name":      CONFIG.ServerName,
		"studio_key":       CONFIG.StudioAPIKey,
		"project_name":     project,
		"checkpoint_ids":   requestData.CheckpointIDs,
		"label":            requestData.Label,
		"expires_in_hours": requestData.ExpiresIn,
		"created_by":       requestingUser.Id,
	}

	payloadBytes, err := json.Marshal(globalPayload)
	if err != nil {
		SendErrorResponse(w, "Failed to prepare request", http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest("POST", constants.HOST+"/share", bytes.NewBuffer(payloadBytes))
	if err != nil {
		SendErrorResponse(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		SendErrorResponse(w, "Failed to connect to global server", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		SendErrorResponse(w, "Failed to read response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

// ShareDownloadHandler streams chunk data for a share link.
// This is the public download endpoint — it validates the token with the global server,
// resolves checkpoint chunk hashes from the local project DB, and streams the data.
func ShareDownloadHandler(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	// Validate token with global server
	req, err := http.NewRequest("GET", constants.HOST+"/share/"+token, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to validate share link", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	var shareData struct {
		ProjectName   string   `json:"project_name"`
		CheckpointIDs []string `json:"checkpoint_ids"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&shareData); err != nil {
		http.Error(w, "Invalid share data", http.StatusInternalServerError)
		return
	}

	checkpointID := r.PathValue("checkpoint_id")

	projectPath := filepath.Join(CONFIG.ProjectsDir, shareData.ProjectName+".clst")
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, "Failed to open project", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Resolve checkpoint IDs to chunk hashes
	targetIDs := shareData.CheckpointIDs
	if checkpointID != "" {
		found := false
		for _, id := range shareData.CheckpointIDs {
			if id == checkpointID {
				found = true
				break
			}
		}
		if !found {
			http.Error(w, "Checkpoint not in share link", http.StatusForbidden)
			return
		}
		targetIDs = []string{checkpointID}
	}

	var allChunkHashes []string
	for _, cpID := range targetIDs {
		var chunks string
		err := tx.Get(&chunks, "SELECT chunks FROM task_checkpoint WHERE id = ?", cpID)
		if err != nil {
			log.Printf("[ShareDownload] Checkpoint %s not found: %v", cpID, err)
			continue
		}
		for _, hash := range splitChunkHashes(chunks) {
			allChunkHashes = append(allChunkHashes, hash)
		}
	}

	if len(allChunkHashes) == 0 {
		http.Error(w, "No chunks found for the shared checkpoints", http.StatusNotFound)
		return
	}

	// Stream chunks using same pattern as StreamChunksHandler
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, chunkHash := range allChunkHashes {
		var chunkData []byte
		err = tx.Get(&chunkData, "SELECT data FROM chunk WHERE hash = ?", chunkHash)
		if err != nil {
			http.Error(w, fmt.Sprintf("Chunk %s not found", chunkHash), http.StatusInternalServerError)
			return
		}
		chunk := chunk_service.Chunk{Hash: chunkHash, Data: chunkData}
		encodedChunk, err := chunk_service.EncodeChunk(chunk)
		if err != nil {
			http.Error(w, "Failed to encode chunk", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(encodedChunk)
		flusher.Flush()
	}

	// Increment download count on global server (fire and forget)
	go func() {
		incReq, err := http.NewRequest("PUT", constants.HOST+"/share/"+token+"/download", nil)
		if err != nil {
			return
		}
		(&http.Client{Timeout: 5 * time.Second}).Do(incReq)
	}()
}

// ShareMetadataHandler returns file metadata for a share link's checkpoints.
// Used by the download page to show file names, sizes, and counts.
func ShareMetadataHandler(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusBadRequest)
		return
	}

	req, err := http.NewRequest("GET", constants.HOST+"/share/"+token, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Failed to validate share link", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	var shareData struct {
		ProjectName   string   `json:"project_name"`
		CheckpointIDs []string `json:"checkpoint_ids"`
		Label         string   `json:"label"`
		ExpiresAt     string   `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&shareData); err != nil {
		http.Error(w, "Invalid share data", http.StatusInternalServerError)
		return
	}

	projectPath := filepath.Join(CONFIG.ProjectsDir, shareData.ProjectName+".clst")
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, "Failed to open project", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	type FileInfo struct {
		CheckpointID string `json:"checkpoint_id"`
		FileName     string `json:"file_name"`
		FileSize     int64  `json:"file_size"`
	}

	var files []FileInfo
	for _, cpID := range shareData.CheckpointIDs {
		var checkpoint struct {
			ID       string `db:"id"`
			TaskID   string `db:"task_id"`
			FileSize int64  `db:"file_size"`
		}
		err := tx.Get(&checkpoint, "SELECT id, task_id, file_size FROM task_checkpoint WHERE id = ?", cpID)
		if err != nil {
			continue
		}

		var task struct {
			Name      string `db:"name"`
			Extension string `db:"extension"`
		}
		err = tx.Get(&task, "SELECT name, extension FROM task WHERE id = ?", checkpoint.TaskID)
		if err != nil {
			continue
		}

		files = append(files, FileInfo{
			CheckpointID: cpID,
			FileName:     task.Name + task.Extension,
			FileSize:     checkpoint.FileSize,
		})
	}

	response := map[string]interface{}{
		"label":      shareData.Label,
		"expires_at": shareData.ExpiresAt,
		"files":      files,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// splitChunkHashes splits a comma-separated chunk hash string into a slice.
func splitChunkHashes(chunks string) []string {
	if chunks == "" {
		return nil
	}
	parts := strings.Split(chunks, ",")
	var result []string
	for _, h := range parts {
		h = strings.TrimSpace(h)
		if h != "" {
			result = append(result, h)
		}
	}
	return result
}
