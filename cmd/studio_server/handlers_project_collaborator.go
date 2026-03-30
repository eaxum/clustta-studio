package main

import (
	"clustta/internal/constants"
	"clustta/internal/repository"
	"clustta/internal/utils"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/jmoiron/sqlx"
)

// AddProjectCollaboratorHandler adds collaborators to a project.
// Requires the requesting user to have the add_user permission in the project.
func AddProjectCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	if !utils.FileExists(projectPath) {
		SendErrorResponse(w, "Project not found", http.StatusNotFound)
		return
	}

	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	// Open project DB and check permission
	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	ptx, err := dbConn.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer ptx.Rollback()

	requestingUser, err := repository.GetUser(ptx, authUser.Id)
	if err != nil {
		SendErrorResponse(w, "You are not a member of this project", http.StatusForbidden)
		return
	}
	if !requestingUser.Role.AddUser {
		SendErrorResponse(w, "You do not have permission to add users", http.StatusForbidden)
		return
	}

	var body struct {
		UserIds []string `json:"user_ids"`
		Role    string   `json:"role"`
	}
	err = json.NewDecoder(r.Body).Decode(&body)
	if err != nil || len(body.UserIds) == 0 {
		SendErrorResponse(w, "user_ids is required", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "Artist"
	}

	type addResult struct {
		UserId  string `json:"user_id"`
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
	}

	results := []addResult{}
	now := time.Now().Unix()

	for _, userId := range body.UserIds {
		if userId == authUser.Id {
			results = append(results, addResult{UserId: userId, Status: "skipped", Message: "Cannot add yourself"})
			continue
		}

		// Verify user is a studio member
		serverUser, exists := Users[userId]
		if !exists {
			results = append(results, addResult{UserId: userId, Status: "error", Message: "User is not a studio member"})
			continue
		}

		// Check if already in the project
		existingUser, _ := repository.GetUser(ptx, userId)
		if existingUser.Id != "" {
			results = append(results, addResult{UserId: userId, Status: "skipped", Message: "Already in project"})
			continue
		}

		// Get the role in the project
		role, err := repository.GetRoleByName(ptx, body.Role)
		if err != nil {
			results = append(results, addResult{UserId: userId, Status: "error", Message: "Role not found"})
			continue
		}

		// Add user to the project .clst file
		_, err = repository.AddKnownUser(ptx, serverUser.Id, serverUser.Email, serverUser.UserName, serverUser.FirstName, serverUser.LastName, role.Id, nil, false)
		if err != nil {
			results = append(results, addResult{UserId: userId, Status: "error", Message: "Failed to add to project"})
			continue
		}

		// Add to studio_project_user for fast lookup
		sudb, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
		if err == nil {
			sudb.Exec("INSERT OR IGNORE INTO studio_project_user (project_name, user_id, added_at) VALUES (?, ?, ?)",
				projectName, userId, now)
			sudb.Close()
		}

		results = append(results, addResult{UserId: userId, Status: "added"})
	}

	ptx.Commit()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(results)
}

// RemoveProjectCollaboratorHandler removes a collaborator from a project.
func RemoveProjectCollaboratorHandler(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	collabUserId := r.PathValue("user_id")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	if !utils.FileExists(projectPath) {
		SendErrorResponse(w, "Project not found", http.StatusNotFound)
		return
	}

	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	ptx, err := dbConn.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer ptx.Rollback()

	requestingUser, err := repository.GetUser(ptx, authUser.Id)
	if err != nil {
		SendErrorResponse(w, "You are not a member of this project", http.StatusForbidden)
		return
	}
	if !requestingUser.Role.RemoveUser {
		SendErrorResponse(w, "You do not have permission to remove users", http.StatusForbidden)
		return
	}

	err = repository.RemoveUser(ptx, collabUserId)
	if err != nil {
		SendErrorResponse(w, "Error removing user from project", http.StatusInternalServerError)
		return
	}

	ptx.Commit()

	// Remove from studio_project_user
	sudb, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
	if err == nil {
		sudb.Exec("DELETE FROM studio_project_user WHERE project_name = ? AND user_id = ?", projectName, collabUserId)
		sudb.Close()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Collaborator removed"})
}

// GetProjectCollaboratorsHandler lists collaborators for a project.
func GetProjectCollaboratorsHandler(w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	if !utils.FileExists(projectPath) {
		SendErrorResponse(w, "Project not found", http.StatusNotFound)
		return
	}

	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer dbConn.Close()

	ptx, err := dbConn.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer ptx.Rollback()

	// Verify the requesting user is in the project
	_, err = repository.GetUser(ptx, authUser.Id)
	if err != nil {
		SendErrorResponse(w, "You are not a member of this project", http.StatusForbidden)
		return
	}

	users, err := repository.GetUsers(ptx)
	if err != nil {
		SendErrorResponse(w, "Error fetching collaborators", http.StatusInternalServerError)
		return
	}

	type collaboratorInfo struct {
		Id        string `json:"id"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		RoleName  string `json:"role_name"`
		RoleId    string `json:"role_id"`
	}

	collaborators := []collaboratorInfo{}
	for _, user := range users {
		collaborators = append(collaborators, collaboratorInfo{
			Id:        user.Id,
			FirstName: user.FirstName,
			LastName:  user.LastName,
			Username:  user.Username,
			Email:     user.Email,
			RoleName:  user.Role.Name,
			RoleId:    user.RoleId,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(collaborators)
}
