package main

import (
	"clustta/internal/server/models"
	"clustta/internal/server/user_service"
	"clustta/internal/utils"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
)

// UpdateCurrentUserHandler updates the authenticated user's basic profile fields.
// Handles PUT /auth/user/profile with first_name, last_name, username and email.
func UpdateCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	firstName, ok := data["first_name"].(string)
	if !ok || strings.TrimSpace(firstName) == "" {
		SendErrorResponse(w, "first_name is required", http.StatusBadRequest)
		return
	}
	lastName, ok := data["last_name"].(string)
	if !ok || strings.TrimSpace(lastName) == "" {
		SendErrorResponse(w, "last_name is required", http.StatusBadRequest)
		return
	}
	username, ok := data["username"].(string)
	if !ok || strings.TrimSpace(username) == "" {
		SendErrorResponse(w, "username is required", http.StatusBadRequest)
		return
	}
	email, ok := data["email"].(string)
	if !ok || strings.TrimSpace(email) == "" {
		SendErrorResponse(w, "email is required", http.StatusBadRequest)
		return
	}
	if !utils.ValidateEmail(email) {
		SendErrorResponse(w, "Invalid email format", http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		log.Printf("Error connecting to database: %v", err)
		SendErrorResponse(w, "Error connecting to database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		SendErrorResponse(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	currentUser, err := user_service.GetUser(tx, authUser.Id)
	if err != nil {
		log.Printf("Error getting user: %v", err)
		SendErrorResponse(w, "Error getting user", http.StatusInternalServerError)
		return
	}

	// Reject username/email changes that collide with another account
	if username != currentUser.UserName {
		exists, err := user_service.UsernameExists(tx, username)
		if err != nil {
			log.Printf("Error checking username: %v", err)
			SendErrorResponse(w, "Error checking username", http.StatusInternalServerError)
			return
		}
		if exists {
			SendErrorResponse(w, "Username already taken", http.StatusConflict)
			return
		}
	}
	if email != currentUser.Email {
		exists, err := user_service.EmailExists(tx, email)
		if err != nil {
			log.Printf("Error checking email: %v", err)
			SendErrorResponse(w, "Error checking email", http.StatusInternalServerError)
			return
		}
		if exists {
			SendErrorResponse(w, "Email already registered", http.StatusConflict)
			return
		}
	}

	params := map[string]interface{}{
		"first_name": firstName,
		"last_name":  lastName,
		"username":   username,
		"email":      email,
	}
	if err := user_service.UpdateUser(tx, authUser.Id, params); err != nil {
		log.Printf("Error updating user: %v", err)
		SendErrorResponse(w, "Error updating user", http.StatusInternalServerError)
		return
	}

	updatedUser, err := user_service.GetUser(tx, authUser.Id)
	if err != nil {
		log.Printf("Error getting user: %v", err)
		SendErrorResponse(w, "Error getting user", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		SendErrorResponse(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	userResponse := models.UserResponse{
		Id:        updatedUser.Id,
		FirstName: updatedUser.FirstName,
		LastName:  updatedUser.LastName,
		UserName:  updatedUser.UserName,
		Email:     updatedUser.Email,
		Active:    updatedUser.Active,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(userResponse); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("{\"message\": \"Failed to encode response\"}"))
	}
}

// UpdateCurrentUserPhotoHandler uploads a new profile photo for the authenticated user.
// Handles POST /auth/user/photo with a multipart "photo" form field.
func UpdateCurrentUserPhotoHandler(w http.ResponseWriter, r *http.Request) {
	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		log.Printf("Error parsing multipart form: %v", err)
		SendErrorResponse(w, "Invalid upload", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("photo")
	if err != nil {
		log.Printf("Error reading photo field: %v", err)
		SendErrorResponse(w, "photo is required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileBytes, err := io.ReadAll(file)
	if err != nil {
		log.Printf("Error reading the file: %v", err)
		SendErrorResponse(w, "Error reading the file", http.StatusInternalServerError)
		return
	}

	resizedImageBytes, err := utils.ResizeImage(fileBytes, 50, 50)
	if err != nil {
		log.Printf("Failed to resize image: %v", err)
		SendErrorResponse(w, "Failed to resize image", http.StatusInternalServerError)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		log.Printf("Error connecting to database: %v", err)
		SendErrorResponse(w, "Error connecting to database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		SendErrorResponse(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	params := map[string]interface{}{
		"photo": resizedImageBytes,
	}
	if err := user_service.UpdateUser(tx, authUser.Id, params); err != nil {
		log.Printf("Error updating user photo: %v", err)
		SendErrorResponse(w, "Error updating user photo", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		SendErrorResponse(w, "Error committing transaction", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{\"message\": \"Photo uploaded successfully\"}"))
}

// ChangeUserPasswordHandler changes the authenticated user's password.
// Handles POST /auth/change-password after verifying the current password.
func ChangeUserPasswordHandler(w http.ResponseWriter, r *http.Request) {
	authUser, ok := getAuthUser(r)
	if !ok {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	password, ok := data["password"].(string)
	if !ok || password == "" {
		SendErrorResponse(w, "password is required", http.StatusBadRequest)
		return
	}
	newPassword, ok := data["new_password"].(string)
	if !ok || newPassword == "" {
		SendErrorResponse(w, "new_password is required", http.StatusBadRequest)
		return
	}
	confirmPassword, ok := data["confirm_password"].(string)
	if !ok || confirmPassword == "" {
		SendErrorResponse(w, "confirm_password is required", http.StatusBadRequest)
		return
	}
	if newPassword != confirmPassword {
		SendErrorResponse(w, "new_password and confirm_password must match", http.StatusBadRequest)
		return
	}
	if isValid, errMsg := user_service.ValidatePassword(newPassword); !isValid {
		SendErrorResponse(w, errMsg, http.StatusBadRequest)
		return
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		log.Printf("Error connecting to database: %v", err)
		SendErrorResponse(w, "Error connecting to database", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	tx, err := db.Beginx()
	if err != nil {
		log.Printf("Error starting transaction: %v", err)
		SendErrorResponse(w, "Error starting transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	_, authenticated, err := user_service.AuthenticateUser(tx, authUser.Email, password)
	if err != nil {
		log.Printf("Error during authentication: %v", err)
		SendErrorResponse(w, "Error during authentication", http.StatusInternalServerError)
		return
	}
	if !authenticated {
		SendErrorResponse(w, "Invalid credentials, check your current password", http.StatusBadRequest)
		return
	}

	if err := user_service.UpdatePassword(tx, authUser.Id, newPassword); err != nil {
		log.Printf("Error changing user password: %v", err)
		SendErrorResponse(w, "Error changing user password", http.StatusInternalServerError)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Error changing user password: %v", err)
		SendErrorResponse(w, "Error changing user password", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{\"message\": \"password changed\"}"))
}
