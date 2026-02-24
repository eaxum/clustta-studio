package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"clustta/internal/server/user_service"
	"clustta/internal/utils"

	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/jmoiron/sqlx"
)

var sessionManager *scs.SessionManager

// InitSessionManager initializes the SCS session manager with SQLite store
func InitSessionManager(sessionDb *sql.DB) {
	sessionManager = scs.New()
	sessionManager.Store = sqlite3store.New(sessionDb)
	sessionManager.Lifetime = 30 * 24 * time.Hour
	sessionManager.Cookie.Name = "session"
	sessionManager.Cookie.SameSite = http.SameSiteLaxMode
	sessionManager.Cookie.Secure = false // Set to true in production with HTTPS
	expireTime := 7 * 24 * time.Hour
	sqlite3store.NewWithCleanupInterval(sessionDb, expireTime)
}

// LoginResponse represents the response from a successful login
type LoginResponse struct {
	Login     bool     `json:"login"`
	User      UserInfo `json:"user"`
	SessionId string   `json:"session_id"`
}

// UserInfo represents the user information returned to the client
type UserInfo struct {
	Id        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Email     string `json:"email"`
}

// RegisterUserHandler handles user registration for the studio server
func RegisterUserHandler(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	firstName, ok := data["first_name"].(string)
	if !ok || firstName == "" {
		SendErrorResponse(w, "first_name is required", http.StatusBadRequest)
		return
	}
	lastName, ok := data["last_name"].(string)
	if !ok || lastName == "" {
		SendErrorResponse(w, "last_name is required", http.StatusBadRequest)
		return
	}
	username, ok := data["username"].(string)
	if !ok || username == "" {
		SendErrorResponse(w, "username is required", http.StatusBadRequest)
		return
	}
	email, ok := data["email"].(string)
	if !ok || email == "" {
		SendErrorResponse(w, "email is required", http.StatusBadRequest)
		return
	}
	password, ok := data["password"].(string)
	if !ok || password == "" {
		SendErrorResponse(w, "password is required", http.StatusBadRequest)
		return
	}
	confirmPassword, ok := data["confirm_password"].(string)
	if !ok || confirmPassword == "" {
		SendErrorResponse(w, "confirm_password is required", http.StatusBadRequest)
		return
	}
	if password != confirmPassword {
		SendErrorResponse(w, "password and confirm_password must match", http.StatusBadRequest)
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

	// Check if email already exists
	exists, err := user_service.EmailExists(tx, email)
	if err != nil {
		SendErrorResponse(w, "Error checking email: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		SendErrorResponse(w, "Email already registered", http.StatusConflict)
		return
	}

	// Check if username already exists
	exists, err = user_service.UsernameExists(tx, username)
	if err != nil {
		SendErrorResponse(w, "Error checking username: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if exists {
		SendErrorResponse(w, "Username already taken", http.StatusConflict)
		return
	}

	// Create the user (auto-activate for studio server - no email verification required)
	createdUser, err := user_service.CreateUser(tx, "", firstName, lastName, username, email, password)
	if err != nil {
		SendErrorResponse(w, "Error creating user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Activate the user immediately (studio servers don't require email verification)
	err = user_service.ActivateUser(tx, createdUser.Id)
	if err != nil {
		SendErrorResponse(w, "Error activating user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Assign role: first user gets admin, subsequent users get 'user' role
	var userCount int
	err = tx.QueryRow("SELECT COUNT(*) FROM user WHERE active = 1").Scan(&userCount)
	if err == nil {
		roleName := "user"
		if userCount <= 1 {
			roleName = "admin"
		}

		var roleId string
		err = tx.QueryRow("SELECT id FROM role WHERE name = ?", roleName).Scan(&roleId)
		if err == nil && roleId != "" {
			_, err = tx.Exec("UPDATE user SET role_id = ? WHERE id = ?", roleId, createdUser.Id)
			if err != nil {
				SendErrorResponse(w, "Error assigning role: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	err = tx.Commit()
	if err != nil {
		SendErrorResponse(w, "Error committing transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	userInfo := UserInfo{
		Id:        createdUser.Id,
		FirstName: createdUser.FirstName,
		LastName:  createdUser.LastName,
		Username:  createdUser.UserName,
		Email:     createdUser.Email,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(userInfo)
}

// LoginUserHandler handles user login for the studio server
func LoginUserHandler(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	emailOrUsername, ok := data["email"].(string)
	if !ok || emailOrUsername == "" {
		SendErrorResponse(w, "email or username is required", http.StatusBadRequest)
		return
	}
	password, ok := data["password"].(string)
	if !ok || password == "" {
		SendErrorResponse(w, "password is required", http.StatusBadRequest)
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

	authUser, authenticated, err := user_service.AuthenticateUser(tx, emailOrUsername, password)
	if err != nil {
		SendErrorResponse(w, "Error during authentication: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !authenticated {
		SendErrorResponse(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Update last presence
	currentTime := utils.GetCurrentTime()
	params := map[string]interface{}{
		"last_presence": currentTime,
	}
	err = user_service.UpdateUser(tx, authUser.Id, params)
	if err != nil {
		SendErrorResponse(w, "Error updating user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = tx.Commit()
	if err != nil {
		SendErrorResponse(w, "Error committing transaction: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Create session
	userInfo := UserInfo{
		Id:        authUser.Id,
		FirstName: authUser.FirstName,
		LastName:  authUser.LastName,
		Username:  authUser.UserName,
		Email:     authUser.Email,
	}
	userData, err := json.Marshal(userInfo)
	if err != nil {
		SendErrorResponse(w, "Error marshaling user data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sessionManager.Put(r.Context(), "user", userData)
	// The session is automatically saved by LoadAndSave middleware
	// Renew the session token to prevent session fixation
	err = sessionManager.RenewToken(r.Context())
	if err != nil {
		SendErrorResponse(w, "Error creating session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get the session token
	sessionToken := sessionManager.Token(r.Context())

	response := LoginResponse{
		Login:     true,
		User:      userInfo,
		SessionId: sessionToken,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// LogoutUserHandler handles user logout
func LogoutUserHandler(w http.ResponseWriter, r *http.Request) {
	err := sessionManager.Destroy(r.Context())
	if err != nil {
		SendErrorResponse(w, "Error destroying session: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Logged out successfully",
	})
}

// UserAuthenticatedHandler checks if the current session is valid
func UserAuthenticatedHandler(w http.ResponseWriter, r *http.Request) {
	userData := sessionManager.Get(r.Context(), "user")
	if userData == nil {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var userInfo UserInfo
	if userBytes, ok := userData.([]byte); ok {
		if err := json.Unmarshal(userBytes, &userInfo); err != nil {
			SendErrorResponse(w, "Invalid session data", http.StatusInternalServerError)
			return
		}
	} else {
		SendErrorResponse(w, "Invalid session data type", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"authenticated": true,
		"user":          userInfo,
	})
}

// CheckEmailExistHandler checks if an email is already registered
func CheckEmailExistHandler(w http.ResponseWriter, r *http.Request) {
	email := r.PathValue("email")
	if email == "" {
		SendErrorResponse(w, "email is required", http.StatusBadRequest)
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

	exists, err := user_service.EmailExists(tx, email)
	if err != nil {
		SendErrorResponse(w, "Error checking email: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"exists": exists})
}

// CheckUsernameExistHandler checks if a username is already taken
func CheckUsernameExistHandler(w http.ResponseWriter, r *http.Request) {
	username := r.PathValue("username")
	if username == "" {
		SendErrorResponse(w, "username is required", http.StatusBadRequest)
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

	exists, err := user_service.UsernameExists(tx, username)
	if err != nil {
		SendErrorResponse(w, "Error checking username: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"exists": exists})
}

// GetCurrentUserHandler returns the current authenticated user's info
func GetCurrentUserHandler(w http.ResponseWriter, r *http.Request) {
	userData := sessionManager.Get(r.Context(), "user")
	if userData == nil {
		SendErrorResponse(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var userInfo UserInfo
	if userBytes, ok := userData.([]byte); ok {
		if err := json.Unmarshal(userBytes, &userInfo); err != nil {
			SendErrorResponse(w, "Invalid session data", http.StatusInternalServerError)
			return
		}
	} else {
		SendErrorResponse(w, "Invalid session data type", http.StatusInternalServerError)
		return
	}

	// Get full user details from database
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

	user, err := user_service.GetUser(tx, userInfo.Id)
	if err != nil {
		SendErrorResponse(w, "Error getting user: "+err.Error(), http.StatusInternalServerError)
		return
	}

	fullUserInfo := UserInfo{
		Id:        user.Id,
		FirstName: user.FirstName,
		LastName:  user.LastName,
		Username:  user.UserName,
		Email:     user.Email,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(fullUserInfo)
}
