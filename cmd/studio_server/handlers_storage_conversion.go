package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"clustta/internal/chunk_service"
	"clustta/internal/server/user_service"
	"clustta/internal/utils"

	"github.com/jmoiron/sqlx"
)

const studioAdminRole = "admin"

type projectStorageConversion struct {
	ProjectName string `json:"project_name"`
	chunk_service.StorageConversionState
}

func requireStudioAdmin(w http.ResponseWriter, r *http.Request) bool {
	user, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return false
	}
	serverUser, exists := Users[user.Id]
	if !exists || serverUser.RoleName != studioAdminRole {
		SendErrorResponse(w, "Only admins can manage project storage", http.StatusForbidden)
		return false
	}
	return true
}

func confirmStorageAdminPassword(w http.ResponseWriter, r *http.Request, password string) bool {
	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return false
	}
	if password == "" {
		SendErrorResponse(w, "Password is required", http.StatusBadRequest)
		return false
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		SendErrorResponse(w, "Failed to open Studio users database", http.StatusInternalServerError)
		return false
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Failed to verify administrator", http.StatusInternalServerError)
		return false
	}
	defer tx.Rollback()

	authenticatedUser, authenticated, err := user_service.AuthenticateUser(tx, authUser.Email, password)
	if err != nil {
		SendErrorResponse(w, "Failed to verify administrator", http.StatusInternalServerError)
		return false
	}
	if !authenticated || authenticatedUser.Id != authUser.Id {
		SendErrorResponse(w, "Invalid password", http.StatusUnauthorized)
		return false
	}

	var roleName string
	err = tx.Get(&roleName, `
		SELECT role.name
		FROM user
		JOIN role ON role.id = user.role_id
		WHERE user.id = ? AND user.active = 1
	`, authUser.Id)
	if err != nil {
		log.Printf("Failed to verify storage conversion administrator role: %v", err)
		SendErrorResponse(w, "Failed to verify administrator", http.StatusInternalServerError)
		return false
	}
	if roleName != studioAdminRole {
		SendErrorResponse(w, "Only admins can manage project storage", http.StatusForbidden)
		return false
	}
	return true
}

func conversionForProject(projectName string) (projectStorageConversion, error) {
	projectPath, err := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if err != nil {
		return projectStorageConversion{}, err
	}
	state, err := chunk_service.GetStorageConversionState(projectPath)
	return projectStorageConversion{ProjectName: projectName, StorageConversionState: state}, err
}

func conversionSummaryForProject(ctx context.Context, projectName string) (projectStorageConversion, error) {
	projectPath, err := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if err != nil {
		return projectStorageConversion{}, err
	}
	state, err := chunk_service.GetStorageConversionSummary(ctx, projectPath)
	return projectStorageConversion{ProjectName: projectName, StorageConversionState: state}, err
}

func GetStorageConversionsHandler(w http.ResponseWriter, r *http.Request) {
	if !requireStudioAdmin(w, r) {
		return
	}
	entries, err := os.ReadDir(CONFIG.ProjectsDir)
	if err != nil {
		SendErrorResponse(w, "Failed to list projects", http.StatusInternalServerError)
		return
	}
	conversions := make([]projectStorageConversion, 0)
	for _, entry := range entries {
		if err := r.Context().Err(); err != nil {
			return
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".clst") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		conversion, err := conversionSummaryForProject(r.Context(), name)
		if err != nil {
			log.Printf("Failed to inspect storage conversion for %q: %v", name, err)
			continue
		}
		conversions = append(conversions, conversion)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversions)
}

func GetStorageConversionHandler(w http.ResponseWriter, r *http.Request) {
	if !requireStudioAdmin(w, r) {
		return
	}
	projectName := r.PathValue("project")
	projectPath, err := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if err != nil || !utils.FileExists(projectPath) {
		SendErrorResponse(w, "Project not found", http.StatusNotFound)
		return
	}
	conversion, err := conversionForProject(projectName)
	if err != nil {
		SendErrorResponse(w, "Failed to read project storage conversion", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(conversion)
}

func StartStorageConversionHandler(w http.ResponseWriter, r *http.Request) {
	if !requireStudioAdmin(w, r) {
		return
	}
	projectName := r.PathValue("project")
	projectPath, err := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if err != nil || !utils.FileExists(projectPath) {
		SendErrorResponse(w, "Project not found", http.StatusNotFound)
		return
	}
	var request struct {
		TargetMode string `json:"target_mode"`
		Password   string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if !confirmStorageAdminPassword(w, r, request.Password) {
		return
	}
	destination := CONFIG.ProjectsDir
	if request.TargetMode == chunk_service.StorageModeDeflated {
		destination = CONFIG.StorageDir
	}
	stats, err := getDiskStats(destination)
	if err != nil {
		SendErrorResponse(w, "Failed to inspect destination storage", http.StatusInternalServerError)
		return
	}
	state, err := chunk_service.StartStorageConversion(projectPath, request.TargetMode, stats.AvailableBytes)
	if err != nil {
		SendErrorResponse(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(projectStorageConversion{ProjectName: projectName, StorageConversionState: state})
}
