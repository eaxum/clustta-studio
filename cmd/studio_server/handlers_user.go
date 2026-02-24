package main

import (
	"clustta/internal/error_service"
	"clustta/internal/server/models"
	"clustta/internal/server/user_service"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jmoiron/sqlx"
)

// GetUserHandler returns user data by email or username.
// This endpoint is used by the desktop client when adding users to a project.
func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	emailOrUsername := r.PathValue("email_or_username")
	if emailOrUsername == "" {
		SendErrorResponse(w, "username or email is required", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		SendErrorResponse(w, "Error connecting to database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Error starting transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	user, err := user_service.GetUserByUsernameOrEmail(tx, emailOrUsername)
	if err != nil {
		if err == sql.ErrNoRows || errors.Is(err, error_service.ErrUserNotFound) {
			SendErrorResponse(w, "User not found", http.StatusNotFound)
			return
		}
		SendErrorResponse(w, "Error getting user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	userResponse := models.UserResponse{
		Id:        user.Id,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		UserName:  user.UserName,
		Email:     user.Email,
		Active:    user.Active,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(userResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("{\"message\": \"Failed to encode response\"}"))
	}
}

// GetUserByIdHandler returns user data by user ID.
// This endpoint is used by the desktop client for user lookups during sync.
func GetUserByIdHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.PathValue("user_id")
	if userId == "" {
		SendErrorResponse(w, "user_id is required", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		SendErrorResponse(w, "Error connecting to database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Error starting transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	user, err := user_service.GetUser(tx, userId)
	if err != nil {
		if err == sql.ErrNoRows {
			SendErrorResponse(w, "User not found", http.StatusNotFound)
			return
		}
		SendErrorResponse(w, "Error getting user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	userResponse := models.UserResponse{
		Id:        user.Id,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		UserName:  user.UserName,
		Email:     user.Email,
		Active:    user.Active,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(userResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("{\"message\": \"Failed to encode response\"}"))
	}
}

// GetUserPhotoHandler returns the photo for a user by their ID.
// Returns the raw photo bytes with image/png content type.
func GetUserPhotoHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.PathValue("user_id")
	if userId == "" {
		SendErrorResponse(w, "user_id is required", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		SendErrorResponse(w, "Error connecting to database: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Error starting transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	var photo []byte
	err = tx.QueryRow("SELECT photo FROM user WHERE id = ?", userId).Scan(&photo)
	if err != nil {
		if err == sql.ErrNoRows {
			SendErrorResponse(w, "User not found", http.StatusNotFound)
			return
		}
		SendErrorResponse(w, "Error getting user photo: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if len(photo) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	w.Write(photo)
}
