package main

import (
	"clustta/internal/auth_service"
	"clustta/internal/chunk_service"
	"clustta/internal/metadata_service"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"
	"clustta/internal/repository/sync_service"
	"clustta/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/zstd"
	"github.com/jmoiron/sqlx"
	"google.golang.org/protobuf/proto"
)

type ErrorStruct struct {
	Message string `json:"error"`
}

// safeProjectPath validates a project name and returns the safe .clst file path.
// Rejects names containing path separators or traversal sequences.
func safeProjectPath(baseDir, projectName string) (string, error) {
	if strings.ContainsAny(projectName, `/\`) || strings.Contains(projectName, "..") || projectName == "" {
		return "", fmt.Errorf("invalid project name")
	}
	resolved := filepath.Join(baseDir, projectName+".clst")
	absBase, _ := filepath.Abs(baseDir)
	absResolved, _ := filepath.Abs(resolved)
	if !strings.HasPrefix(absResolved, absBase+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes base directory")
	}
	return resolved, nil
}

type dataStruct struct {
	UserId string `json:"user_id"`
}
type PingResponse struct {
	Status string `json:"status"`
}

type StudioKeyResponse struct {
	StudioKey string `json:"studio_key"`
}

func PingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := PingResponse{Status: "active"}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type VersionResponse struct {
	Version string `json:"version"`
}

type UsageResponse struct {
	ProjectCount          int   `json:"project_count"`
	StorageBytes          int64 `json:"storage_bytes"`
	StorageAvailableBytes int64 `json:"storage_available_bytes"`
	StorageTotalBytes     int64 `json:"storage_total_bytes"`
}

func VersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := VersionResponse{Version: Version}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetUsageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	UserData := r.Header.Get("UserData")
	if UserData == "" {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var requestingUser UserInfo
	if err := json.Unmarshal([]byte(UserData), &requestingUser); err != nil {
		SendErrorResponse(w, "Invalid user data", http.StatusBadRequest)
		return
	}
	serverUser := Users[requestingUser.Id]
	if serverUser.RoleName != "admin" {
		SendErrorResponse(w, "Only admins can view studio usage", http.StatusForbidden)
		return
	}

	projectsDir := CONFIG.ProjectsDir
	if projectsDir == "" {
		SendErrorResponse(w, "Projects directory is not configured", http.StatusInternalServerError)
		return
	}

	var storageBytes int64
	projectCount := 0
	cleanProjectsDir := filepath.Clean(projectsDir)
	err := filepath.WalkDir(projectsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.IsDir() {
			storageBytes += info.Size()
			if filepath.Dir(filepath.Clean(path)) == cleanProjectsDir && strings.EqualFold(filepath.Ext(info.Name()), ".clst") {
				projectCount++
			}
		}

		return nil
	})
	if err != nil {
		log.Printf("[GetUsage] failed to inspect projects directory %q: %v", projectsDir, err)
		SendErrorResponse(w, "Failed to inspect projects directory", http.StatusInternalServerError)
		return
	}

	diskStats, err := getDiskStats(projectsDir)
	if err != nil {
		log.Printf("[GetUsage] failed to inspect disk for %q: %v", projectsDir, err)
		SendErrorResponse(w, "Failed to inspect storage volume", http.StatusInternalServerError)
		return
	}

	response := UsageResponse{
		ProjectCount:          projectCount,
		StorageBytes:          storageBytes,
		StorageAvailableBytes: diskStats.AvailableBytes,
		StorageTotalBytes:     diskStats.TotalBytes,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetStudioKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := StudioKeyResponse{StudioKey: CONFIG.StudioAPIKey}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// StudioInfoResponse represents studio metadata for client discovery
type StudioInfoResponse struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Url    string `json:"url"`
	AltUrl string `json:"alt_url"`
}

// GetStudioInfoHandler returns studio metadata for client discovery
func GetStudioInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Generate a deterministic ID from the server name if not configured
	studioId := CONFIG.ServerName
	if studioId == "" {
		studioId = "private-studio"
	}

	response := StudioInfoResponse{
		Id:     studioId,
		Name:   CONFIG.ServerName,
		Url:    CONFIG.ServerURL,
		AltUrl: CONFIG.ServerAltURL,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// studioNameRegex enforces the allowed character set for a self-hosted studio name.
var studioNameRegex = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// reservedStudioNames are values that must not be used as a studio name.
var reservedStudioNames = map[string]struct{}{
	"personal": {},
	"admin":    {},
	"clustta":  {},
}

// UpdateStudioInfoHandler updates the local studio config (name, URL, alt URL, port)
// and persists it to studio_config.json. Admin role required. Self-hosted only —
// the global Clustta server is not contacted.
func UpdateStudioInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	UserData := r.Header.Get("UserData")
	if UserData == "" {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var requestingUser UserInfo
	if err := json.Unmarshal([]byte(UserData), &requestingUser); err != nil {
		SendErrorResponse(w, "Invalid user data", http.StatusBadRequest)
		return
	}
	serverUser := Users[requestingUser.Id]
	if serverUser.RoleName != "admin" {
		SendErrorResponse(w, "Only admins can update studio info", http.StatusForbidden)
		return
	}

	var payload struct {
		Name   *string `json:"name"`
		Url    *string `json:"url"`
		AltUrl *string `json:"alt_url"`
		Port   *string `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	configMutex.Lock()
	defer configMutex.Unlock()

	updated := CONFIG

	if payload.Name != nil {
		name := strings.TrimSpace(*payload.Name)
		if name == "" {
			SendErrorResponse(w, "name must not be empty", http.StatusBadRequest)
			return
		}
		if len(name) < 2 || len(name) > 64 {
			SendErrorResponse(w, "name must be 2-64 characters", http.StatusBadRequest)
			return
		}
		if !studioNameRegex.MatchString(name) {
			SendErrorResponse(w, "name may only contain letters, digits, '.', '_', '-'", http.StatusBadRequest)
			return
		}
		if _, reserved := reservedStudioNames[strings.ToLower(name)]; reserved {
			SendErrorResponse(w, "name is reserved", http.StatusBadRequest)
			return
		}
		updated.ServerName = name
	}

	if payload.Url != nil {
		updated.ServerURL = strings.TrimSpace(*payload.Url)
	}
	if payload.AltUrl != nil {
		updated.ServerAltURL = strings.TrimSpace(*payload.AltUrl)
	}
	if payload.Port != nil {
		port := strings.TrimSpace(*payload.Port)
		if port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				SendErrorResponse(w, "port must be a number between 1 and 65535", http.StatusBadRequest)
				return
			}
		}
		updated.Port = port
	}

	if err := saveConfig(&updated); err != nil {
		log.Printf("[UpdateStudioInfo] saveConfig failed: %v", err)
		SendErrorResponse(w, "Failed to persist studio config", http.StatusInternalServerError)
		return
	}

	CONFIG = updated

	studioId := CONFIG.ServerName
	if studioId == "" {
		studioId = "private-studio"
	}
	response := StudioInfoResponse{
		Id:     studioId,
		Name:   CONFIG.ServerName,
		Url:    CONFIG.ServerURL,
		AltUrl: CONFIG.ServerAltURL,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// GetStudioUsersHandler returns all studio users with their roles
func GetStudioUsersHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	UserData := r.Header.Get("UserData")
	if UserData == "" {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	users, err := GetStudioUsers()
	if err != nil {
		log.Printf("[GetStudioUsers] Error: %v", err)
		log.Printf("Error fetching studio users: %v", err)
		SendErrorResponse(w, "Error fetching studio users", http.StatusInternalServerError)
		return
	}

	log.Printf("[GetStudioUsers] Returning %d users", len(users))

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(users)
}

// ChangeStudioUserRoleHandler changes a user's role in the studio
func ChangeStudioUserRoleHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	UserData := r.Header.Get("UserData")
	if UserData == "" {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Check the requesting user is admin
	var requestingUser UserInfo
	if err := json.Unmarshal([]byte(UserData), &requestingUser); err != nil {
		SendErrorResponse(w, "Invalid user data", http.StatusInternalServerError)
		return
	}
	serverUser := Users[requestingUser.Id]
	if serverUser.RoleName != "admin" {
		SendErrorResponse(w, "Only admins can change user roles", http.StatusForbidden)
		return
	}

	var data map[string]string
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	userId, ok := data["user_id"]
	if !ok || userId == "" {
		SendErrorResponse(w, "user_id is required", http.StatusBadRequest)
		return
	}
	roleName, ok := data["role_name"]
	if !ok || roleName == "" {
		SendErrorResponse(w, "role_name is required", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		log.Printf("Error connecting to database: %v", err)
		SendErrorResponse(w, "Error connecting to database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Look up the role ID by name
	var roleId string
	err = db.QueryRow("SELECT id FROM role WHERE name = ?", strings.ToLower(roleName)).Scan(&roleId)
	if err != nil {
		SendErrorResponse(w, "Invalid role name: "+roleName, http.StatusBadRequest)
		return
	}

	// Update the user's role
	result, err := db.Exec("UPDATE user SET role_id = ? WHERE id = ?", roleId, userId)
	if err != nil {
		log.Printf("Error updating user role: %v", err)
		SendErrorResponse(w, "Error updating user role", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		SendErrorResponse(w, "User not found", http.StatusNotFound)
		return
	}

	// Refresh the in-memory users map
	_ = GetUsers()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Role updated successfully"})
}

// RemoveStudioUserHandler removes a user from the studio
func RemoveStudioUserHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	UserData := r.Header.Get("UserData")
	if UserData == "" {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Check the requesting user is admin
	var requestingUser UserInfo
	if err := json.Unmarshal([]byte(UserData), &requestingUser); err != nil {
		SendErrorResponse(w, "Invalid user data", http.StatusInternalServerError)
		return
	}
	serverUser := Users[requestingUser.Id]
	if serverUser.RoleName != "admin" {
		SendErrorResponse(w, "Only admins can remove users", http.StatusForbidden)
		return
	}

	userId := r.PathValue("user_id")
	if userId == "" {
		SendErrorResponse(w, "user_id is required", http.StatusBadRequest)
		return
	}

	// Prevent admin from removing themselves
	if userId == requestingUser.Id {
		SendErrorResponse(w, "Cannot remove yourself", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		log.Printf("Error connecting to database: %v", err)
		SendErrorResponse(w, "Error connecting to database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	// Soft-delete the user
	result, err := db.Exec("UPDATE user SET is_deleted = 1, active = 0 WHERE id = ?", userId)
	if err != nil {
		log.Printf("Error removing user: %v", err)
		SendErrorResponse(w, "Error removing user", http.StatusInternalServerError)
		return
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		SendErrorResponse(w, "User not found", http.StatusNotFound)
		return
	}

	// Remove from in-memory map
	delete(Users, userId)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "User removed successfully"})
}

func PostProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Clustta-Agent") != "Clustta/0.2" {
		http.Error(w, "Invalid Client", 500)
		return
	}

	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	if utils.FileExists(projectPath) {
		http.Error(w, "Project Already Exist", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to create a project", 400)
		return
	}
	user = auth_service.User{
		Id:        user.Id,
		Email:     serverUser.Email,
		Username:  serverUser.UserName,
		FirstName: serverUser.FirstName,
		LastName:  serverUser.LastName,
	}

	projectInfo, err := repository.CreateProject(projectPath, "", "", "No Template", "", user)
	if err != nil {
		if utils.FileExists(projectPath) {
			journal := projectPath + "-journal"
			err := os.Remove(projectPath)
			if err != nil {
				log.Printf("Request error: %v", err)
				http.Error(w, "Internal server error", 400)
				return
			}
			if utils.FileExists(journal) {
				err = os.Remove(journal)
				if err != nil {
					log.Printf("Request error: %v", err)
					http.Error(w, "Internal server error", 400)
					return
				}
			}

		}
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func RenameProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	var data map[string]string
	json.NewDecoder(r.Body).Decode(&data)
	newProjectName, ok := data["name"]
	if !ok || newProjectName == "" {
		http.Error(w, "name is required", 400)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.RenameProject(projectPath, "", newProjectName, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	newProjectPath := filepath.Join(filepath.Dir(projectPath), newProjectName+".clst")

	projectInfo, err := repository.GetProjectInfo(newProjectPath, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

// DeleteProjectHandler permanently deletes a project from the studio.
// Only admins can delete projects. This operation cannot be undone.
func DeleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 404)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to delete this project", 403)
		return
	}

	// Delete the .clst file
	if err := os.Remove(projectPath); err != nil {
		log.Printf("Failed to delete project: %v", err)
		http.Error(w, "Failed to delete project", 500)
		return
	}

	// Also delete journal file if exists
	journal := projectPath + "-journal"
	if utils.FileExists(journal) {
		os.Remove(journal)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Project deleted successfully"})
}

func ToggleProjectCloseHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.ToggleCloseProject(projectPath, "", user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func SetProjectIconHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	var data map[string]string
	json.NewDecoder(r.Body).Decode(&data)
	newProjectIcon, ok := data["icon"]
	if !ok || newProjectIcon == "" {
		http.Error(w, "icon is required", 400)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.SetIcon(projectPath, "", newProjectIcon, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func SetProjectIgnoreListHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, projectName)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	var ignoreList []string
	json.NewDecoder(r.Body).Decode(&ignoreList)

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.SetIgnoreList(projectPath, "", ignoreList, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func GetProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, r.PathValue("project"))
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	project := repository.ProjectInfo{}

	//check if project exists
	if !utils.FileExists(projectPath) {
		http.Error(w, "project does not exist", 500)
		return
	}

	UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	userInProject, err := repository.UserInProject(projectPath, UserId)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	if userInProject {
		projectInfo, err := repository.GetProjectInfo(projectPath, user)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		project = projectInfo
	} else {
		http.Error(w, "user not in project", 400)
		return
	}
	objJson, _ := json.Marshal(project)
	w.Write(objJson)
}

func GetProjectSyncTokenHandler(
	w http.ResponseWriter, r *http.Request) {
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, r.PathValue("project"))
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}

	UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	userInProject, err := repository.UserInProject(projectPath, UserId)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	if userInProject {
		db, err := utils.OpenDb(projectPath)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		defer db.Close()
		tx, err := db.Beginx()
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		defer tx.Rollback()

		syncToken, err := utils.GetProjectSyncToken(tx)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		// jsonStr := map[string]string{"sync_token": syncToken}
		// objJson, _ := json.Marshal(jsonStr)

		byteStr := []byte(syncToken)
		w.Write(byteStr)
	} else {
		http.Error(w, "user not in project", 400)
		return
	}
}

func GetProjectsHandler(
	w http.ResponseWriter, r *http.Request) {
	projectFolder := CONFIG.ProjectsDir

	extension := "clst"
	projects := []repository.ProjectInfo{}

	// Read the directory
	entries, err := os.ReadDir(projectFolder)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	// UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")
	user := auth_service.User{}
	err = json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	println("UserId", user.Id)

	// Iterate over the directory entries
	for _, entry := range entries {
		// Check if the entry is a file and has the specified extension
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), extension) {
			projectPath := filepath.Join(projectFolder, entry.Name())

			fileInfo, err := entry.Info()
			if err != nil {
				log.Printf("Request error: %v", err)
				http.Error(w, "Internal server error", 500)
				return
			}
			if fileInfo.Size() == 0 {
				os.Remove(projectPath)
			}

			valid, err := repository.VerifyProjectIntegrity(projectPath)
			if !valid || err != nil {
				continue
			}

			userInProject, err := repository.UserInProject(projectPath, user.Id)
			if err != nil {
				log.Printf("Request error: %v", err)
				http.Error(w, "Internal server error", 400)
				return
			}
			if userInProject {
				projectInfo, err := repository.GetProjectInfo(projectPath, user)
				if err != nil {
					log.Printf("Request error: %v", err)
					http.Error(w, "Internal server error", 400)
					return
				}
				projects = append(projects, projectInfo)
			}

		}
	}

	// projectsData := map[string]interface{}{
	// 	"projects": projects,
	// }
	objJson, _ := json.Marshal(projects)
	w.Write(objJson)
}

func GetDataHandler(
	w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	if err := repository.UpdateProject(projectPath); err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	db, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data dataStruct
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	userData, err := sync_service.LoadUserDataPb(tx, data.UserId)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	compressedData, err := zstd.CompressLevel(nil, userData, 3)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	_, err = w.Write(compressedData)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

}

func PostDataHandler(
	w http.ResponseWriter, r *http.Request) {
	authUser, ok := getAuthUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	if err := repository.UpdateProject(projectPath); err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	db, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	body, err := io.ReadAll(io.LimitReader(r.Body, 50<<20))
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	decompressedData, err := zstd.Decompress(nil, body)
	if err != nil {
		http.Error(w, "Failed to decompress data", 500)
		return
	}
	if len(decompressedData) > 200<<20 {
		http.Error(w, "Decompressed data exceeds size limit", 413)
		return
	}

	userDataPb := repositorypb.ProjectData{}
	err = proto.Unmarshal(decompressedData, &userDataPb)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	requestData := sync_service.ProjectData{
		ProjectPreview:      userDataPb.ProjectPreview,
		CollectionTypes:     repository.FromPbCollectionTypes(userDataPb.CollectionTypes),
		Collections:         repository.FromPbCollections(userDataPb.Collections),
		CollectionAssignees: repository.FromPbCollectionAssignees(userDataPb.CollectionAssignees),

		AssetTypes:             repository.FromPbAssetTypes(userDataPb.AssetTypes),
		Assets:                 repository.FromPbAssets(userDataPb.Assets),
		AssetsCheckpoints:      repository.FromPbCheckpoints(userDataPb.AssetsCheckpoints),
		AssetDependencies:      repository.FromPbAssetDependencies(userDataPb.AssetDependencies),
		CollectionDependencies: repository.FromPbCollectionDependencies(userDataPb.CollectionDependencies),

		Statuses:        repository.FromPbStatuses(userDataPb.Statuses),
		DependencyTypes: repository.FromPbDependencyTypes(userDataPb.DependencyTypes),

		Users: repository.FromPbUsers(userDataPb.Users),
		Roles: repository.FromPbRoles(userDataPb.Roles),

		Templates: repository.FromPbTemplates(userDataPb.Templates),

		Workflows:           repository.FromPbWorkflows(userDataPb.Workflows),
		WorkflowLinks:       repository.FromPbWorkflowLinks(userDataPb.WorkflowLinks),
		WorkflowCollections: repository.FromPbWorkflowCollections(userDataPb.WorkflowCollections),
		WorkflowAssets:      repository.FromPbWorkflowAssets(userDataPb.WorkflowAssets),

		Tags:       repository.FromPbTags(userDataPb.Tags),
		AssetsTags: repository.FromPbAssetTags(userDataPb.AssetsTags),

		Tombs: repository.FromPbTombs(userDataPb.Tomb),

		IntegrationProjects:           repository.FromPbIntegrationProjects(userDataPb.IntegrationProjects),
		IntegrationCollectionMappings: repository.FromPbIntegrationCollectionMappings(userDataPb.IntegrationCollectionMappings),
		IntegrationAssetMappings:      repository.FromPbIntegrationAssetMappings(userDataPb.IntegrationAssetMappings),
	}

	conflictResult, err := sync_service.CheckForConflicts(tx, requestData)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 500)
		return
	}

	if !conflictResult.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(conflictResult)
		return
	}

	if err := sync_service.AuthorizeProjectDataWrite(tx, authUser.Id, false, requestData); err != nil {
		log.Printf("AUDIT: user=%s action=sync_denied project=%s reason=%v", authUser.Id, project, err)
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}

	err = sync_service.WriteProjectData(tx, requestData, true)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	err = repository.UpdateUsersPhoto(tx)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	err = repository.AddItemsToTomb(tx, requestData.Tombs)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	newSyncToken := utils.GenerateToken()
	err = utils.SetProjectSyncToken(tx, newSyncToken)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	utils.RunPassiveCheckpoint(db)

	// Notify integration listeners when sync touched integration_project rows
	// so they reconcile within seconds instead of waiting for the next tick.
	if ListenerManager != nil && len(requestData.IntegrationProjects) > 0 {
		seen := make(map[string]struct{}, len(requestData.IntegrationProjects))
		for _, ip := range requestData.IntegrationProjects {
			if ip.IntegrationId == "" {
				continue
			}
			if _, dup := seen[ip.IntegrationId]; dup {
				continue
			}
			seen[ip.IntegrationId] = struct{}{}
			integrationId := ip.IntegrationId
			go func() {
				if err := ListenerManager.Restart(context.Background(), "", integrationId); err != nil {
					log.Printf("integration listener restart failed integration=%s err=%v", integrationId, err)
				}
			}()
		}
	}
}

// UpdateStatusHandler updates asset statuses in a studio project.
// Accepts a JSON body with "assets" (array of {asset_id, status_id}).
func UpdateStatusHandler(w http.ResponseWriter, r *http.Request) {
	authUser, ok := getAuthUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", http.StatusNotFound)
		return
	}

	var body struct {
		Assets []struct {
			AssetId  string `json:"asset_id"`
			StatusId string `json:"status_id"`
		} `json:"assets"`
	}
	err := json.NewDecoder(r.Body).Decode(&body)
	if err != nil || len(body.Assets) == 0 {
		http.Error(w, "assets array is required", http.StatusBadRequest)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, "Error opening project file", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, "Error opening transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	type statusResult struct {
		AssetId  string `json:"asset_id"`
		StatusId string `json:"status_id"`
		Status   string `json:"status"`
		Message  string `json:"message,omitempty"`
	}
	request := metadata_service.AssetRequest{Assets: make([]metadata_service.AssetPatch, 0, len(body.Assets))}
	results := make([]statusResult, 0, len(body.Assets))
	for _, item := range body.Assets {
		statusId := item.StatusId
		request.Assets = append(request.Assets, metadata_service.AssetPatch{Id: item.AssetId, StatusId: &statusId})
		results = append(results, statusResult{AssetId: item.AssetId, StatusId: item.StatusId, Status: "updated"})
	}
	if _, err = metadata_service.ApplyAssets(tx, authUser.Id, request); err != nil {
		writeMutationError(w, err)
		return
	}
	err = tx.Commit()
	if err != nil {
		http.Error(w, "Error committing status changes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(results)
}

func GetChunksHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	type chunksStruct struct {
		Chunks []string `json:"chunks"`
	}
	var data chunksStruct
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	chunks := []chunk_service.Chunk{}
	for _, chunkHash := range data.Chunks {
		var chunkData []byte
		err = tx.Get(&chunkData, "SELECT data FROM chunk WHERE hash = ?", chunkHash)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		chunk := chunk_service.Chunk{
			Hash: chunkHash,
			Data: chunkData,
		}
		chunks = append(chunks, chunk)
	}

	encodedChunks, err := chunk_service.EncodeChunks(chunks)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	w.Write(encodedChunks)
}

func StreamChunksHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		log.Printf("Stream chunks: failed to clear write deadline: %v", err)
	}

	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	type chunksStruct struct {
		Chunks []string `json:"chunks"`
	}
	var data chunksStruct
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	// Enable streaming response
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Transfer-Encoding", "chunked")

	if _, ok := w.(http.Flusher); !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, chunkHash := range data.Chunks {
		if err := r.Context().Err(); err != nil {
			log.Printf("Stream chunks: request cancelled for project %s after client disconnect: %v", project, err)
			return
		}

		var chunkData []byte
		err = tx.Get(&chunkData, "SELECT data FROM chunk WHERE hash = ?", chunkHash)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		chunk := chunk_service.Chunk{
			Hash: chunkHash,
			Data: chunkData,
		}
		encodedChunk, err := chunk_service.EncodeChunk(chunk)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		// Send chunk data to client
		if _, err := w.Write(encodedChunk); err != nil {
			log.Printf("Stream chunks: write failed for project %s chunk %s: %v", project, chunkHash, err)
			return
		}
		if err := rc.Flush(); err != nil {
			log.Printf("Stream chunks: flush failed for project %s chunk %s: %v", project, chunkHash, err)
			return
		}
	}
}

func PostChunksHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	chunks, err := io.ReadAll(io.LimitReader(r.Body, 100<<20))
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	failedChunks, err := chunk_service.WriteChunks(projectPath, chunks)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	chunk_service.RunPassiveCheckpointForProject(projectPath)

	data := map[string]interface{}{
		"failed_chunks": failedChunks,
	}
	objJson, err := json.Marshal(data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	w.Write(objJson)
}

func ChunksMissingHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := getAuthUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data []string
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	missingChunks := []string{}
	seenChunks := make(map[string]bool)
	for _, chunkHash := range data {
		if chunk_service.ChunkExists(chunkHash, tx, seenChunks) {
			continue
		}
		missingChunks = append(missingChunks, chunkHash)
	}

	objJson, err := json.Marshal(missingChunks)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	w.Write(objJson)
}

func GetChunksInfoHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data []string
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}

	chunksInfo := []chunk_service.ChunkInfo{}
	// seenChunks := make(map[string]bool)
	for _, chunkHash := range data {
		chunkInfo, err := chunk_service.GetChunkInfo(tx, chunkHash)
		if err != nil {
			log.Printf("Request error: %v", err)
			http.Error(w, "Internal server error", 400)
			return
		}
		chunksInfo = append(chunksInfo, chunkInfo)
	}

	objJson, err := json.Marshal(chunksInfo)
	if err != nil {
		log.Printf("Request error: %v", err)
		http.Error(w, "Internal server error", 400)
		return
	}
	w.Write(objJson)
}
