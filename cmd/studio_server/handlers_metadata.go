package main

import (
	"clustta/internal/metadata_service"
	"clustta/internal/utils"
	"encoding/json"
	"errors"
	"github.com/jmoiron/sqlx"
	"net/http"
)

func writeMutationError(w http.ResponseWriter, e error) {
	status := http.StatusUnprocessableEntity
	if errors.Is(e, metadata_service.ErrForbidden) {
		status = http.StatusForbidden
	}
	http.Error(w, e.Error(), status)
}
func openMutationProject(w http.ResponseWriter, r *http.Request) (string, *sqlx.DB, bool) {
	u, ok := getAuthUser(r)
	if !ok {
		http.Error(w, "Unauthorized", 401)
		return "", nil, false
	}
	path, e := safeProjectPath(CONFIG.ProjectsDir, r.PathValue("project"))
	if e != nil {
		http.Error(w, "Invalid project name", 400)
		return "", nil, false
	}
	if !utils.FileExists(path) {
		http.Error(w, "Project Not Found", 404)
		return "", nil, false
	}
	db, e := utils.OpenDb(path)
	if e != nil {
		http.Error(w, "Error opening project file", 500)
		return "", nil, false
	}
	return u.Id, db, true
}
func PatchAssetsHandler(w http.ResponseWriter, r *http.Request) {
	id, db, ok := openMutationProject(w, r)
	if !ok {
		return
	}
	defer db.Close()
	var req metadata_service.AssetRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	tx, e := db.Beginx()
	if e != nil {
		http.Error(w, "Internal server error", 500)
		return
	}
	defer tx.Rollback()
	out, e := metadata_service.ApplyAssets(tx, id, req)
	if e != nil {
		writeMutationError(w, e)
		return
	}
	if e = tx.Commit(); e != nil {
		http.Error(w, "Internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
func PatchCollectionsHandler(w http.ResponseWriter, r *http.Request) {
	id, db, ok := openMutationProject(w, r)
	if !ok {
		return
	}
	defer db.Close()
	var req metadata_service.CollectionRequest
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	tx, e := db.Beginx()
	if e != nil {
		http.Error(w, "Internal server error", 500)
		return
	}
	defer tx.Rollback()
	out, e := metadata_service.ApplyCollections(tx, id, req)
	if e != nil {
		writeMutationError(w, e)
		return
	}
	if e = tx.Commit(); e != nil {
		http.Error(w, "Internal server error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
