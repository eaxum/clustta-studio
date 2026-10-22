package main

import (
	"clustta/internal/constants"
	"clustta/internal/server/api_token_service"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type contextKey string

const apiTokenContextKey contextKey = "api_token"
const apiUserContextKey contextKey = "api_user"

func ApiTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del("UserData")
		r.Header.Del("UserId")

		if isPublicRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		user, rawToken, ok := authenticateRequest(r)
		if !ok {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), apiUserContextKey, user)
		if rawToken != "" {
			ctx = context.WithValue(ctx, apiTokenContextKey, rawToken)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isPublicRequest(r *http.Request) bool {
	if r.Method == http.MethodOptions {
		return true
	}
	if r.Method == http.MethodPost && (r.URL.Path == "/auth/register" || r.URL.Path == "/auth/login") {
		return true
	}
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/ping" || r.URL.Path == "/version" || r.URL.Path == "/studio-key" || r.URL.Path == "/studio-info" {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/auth/email-exists/") ||
		strings.HasPrefix(r.URL.Path, "/auth/username-exists/")
}

func authenticateRequest(r *http.Request) (UserInfo, string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		if !strings.HasPrefix(authHeader, "Bearer ") {
			return UserInfo{}, "", false
		}
		rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if rawToken == "" {
			return UserInfo{}, "", false
		}
		user, ok := authenticateBearerToken(rawToken)
		return user, rawToken, ok
	}

	user, ok := authenticateLocalSession(r)
	return user, "", ok
}

func authenticateBearerToken(rawToken string) (UserInfo, bool) {
	db, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
	if err != nil {
		return UserInfo{}, false
	}
	defer db.Close()

	userId, err := api_token_service.ValidateToken(db, rawToken)
	if err == nil {
		return loadActiveLocalUser(db, userId)
	}
	if CONFIG.Private {
		return UserInfo{}, false
	}
	user, ok := resolveGlobalToken(rawToken)
	if !ok {
		return UserInfo{}, false
	}
	_, exists, err := getStudioUser(user.Id)
	if err != nil || !exists {
		return UserInfo{}, false
	}
	return user, true
}

func authenticateLocalSession(r *http.Request) (UserInfo, bool) {
	userData := sessionManager.Get(r.Context(), "user")
	userBytes, ok := userData.([]byte)
	if !ok {
		return UserInfo{}, false
	}

	var sessionUser UserInfo
	if err := json.Unmarshal(userBytes, &sessionUser); err != nil || sessionUser.Id == "" {
		return UserInfo{}, false
	}

	db, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
	if err != nil {
		return UserInfo{}, false
	}
	defer db.Close()
	return loadActiveLocalUser(db, sessionUser.Id)
}

func loadActiveLocalUser(db *sqlx.DB, userId string) (UserInfo, bool) {
	var user UserInfo
	err := db.QueryRow(`
		SELECT id, first_name, last_name, username, email
		FROM user
		WHERE id = ? AND active = 1 AND is_deleted = 0
	`, userId).Scan(&user.Id, &user.FirstName, &user.LastName, &user.Username, &user.Email)
	return user, err == nil
}

func getAuthUser(r *http.Request) (UserInfo, bool) {
	user, ok := r.Context().Value(apiUserContextKey).(UserInfo)
	return user, ok && user.Id != ""
}

func resolveGlobalToken(token string) (UserInfo, bool) {
	req, err := http.NewRequest(http.MethodGet, constants.HOST+"/auth/authenticated", nil)
	if err != nil {
		return UserInfo{}, false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Global authentication failed: %v", err)
		return UserInfo{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return UserInfo{}, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UserInfo{}, false
	}

	var result struct {
		Authenticated bool     `json:"authenticated"`
		User          UserInfo `json:"user"`
	}
	if err := json.Unmarshal(body, &result); err != nil || !result.Authenticated || result.User.Id == "" {
		return UserInfo{}, false
	}
	return result.User, true
}
