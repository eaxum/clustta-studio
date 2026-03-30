package main

import (
	"clustta/internal/constants"
	"clustta/internal/server/api_token_service"
	"clustta/internal/server/user_service"
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jmoiron/sqlx"
)

type contextKey string

const apiTokenContextKey contextKey = "api_token"
const apiUserContextKey contextKey = "api_user"

// ApiTokenMiddleware checks for a Bearer token in the Authorization header.
// If found, it validates against the api_token table and injects user data
// into the context so downstream handlers work transparently.
// If no bearer token is present, the request passes through for cookie-based auth.
func ApiTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			if rawToken != "" {
				db, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
				if err != nil {
					http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
					return
				}
				defer db.Close()

				userId, err := api_token_service.ValidateToken(db, rawToken)
				if err != nil {
					http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
					return
				}

				tx, err := db.Beginx()
				if err != nil {
					http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
					return
				}
				defer tx.Rollback()

				user, err := user_service.GetUser(tx, userId)
				if err != nil {
					http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
					return
				}

				userInfo := UserInfo{
					Id:        user.Id,
					FirstName: user.FirstName,
					LastName:  user.LastName,
					Username:  user.UserName,
					Email:     user.Email,
				}

				// Serialize and store in context (same format as session data)
				userBytes, _ := json.Marshal(userInfo)
				ctx := context.WithValue(r.Context(), apiUserContextKey, userBytes)
				ctx = context.WithValue(ctx, apiTokenContextKey, rawToken)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// getAuthUser extracts the authenticated user from context.
// Checks API token context first, then falls back to SCS session.
func getAuthUser(r *http.Request) (UserInfo, bool) {
	// Check API token context first
	if userBytes, ok := r.Context().Value(apiUserContextKey).([]byte); ok {
		var user UserInfo
		if err := json.Unmarshal(userBytes, &user); err == nil {
			return user, true
		}
	}

	// Fall back to session
	userData := sessionManager.Get(r.Context(), "user")
	if userData == nil {
		return UserInfo{}, false
	}
	if userBytes, ok := userData.([]byte); ok {
		var user UserInfo
		if err := json.Unmarshal(userBytes, &user); err == nil {
			return user, true
		}
	}
	return UserInfo{}, false
}
