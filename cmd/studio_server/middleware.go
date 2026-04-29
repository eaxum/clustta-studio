package main

import (
	"clustta/internal/constants"
	"clustta/internal/server/api_token_service"
	"clustta/internal/server/user_service"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
)

type contextKey string

const apiTokenContextKey contextKey = "api_token"
const apiUserContextKey contextKey = "api_user"

// tokenCacheEntry stores a validated user resolved from the global server.
type tokenCacheEntry struct {
	user      UserInfo
	expiresAt time.Time
}

var (
	tokenCache   = map[string]tokenCacheEntry{}
	tokenCacheMu sync.RWMutex
	cacheTTL     = 5 * time.Minute
)

var publicPaths = map[string]bool{
	"/ping":        true,
	"/version":     true,
	"/studio-key":  true,
	"/studio-info": true,
}

// ApiTokenMiddleware authenticates incoming requests via Bearer token.
// It first attempts to validate the token against the local api_token table
// (used in private mode where users register directly with the studio).
// If local validation fails and the studio is non-private (discoverable),
// it falls back to resolveGlobalToken which validates the token against the
// global Clustta server and caches the resolved user identity for subsequent requests.
// If no Bearer token is present, the request passes through for cookie-based auth.
func ApiTokenMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			rawToken := strings.TrimPrefix(authHeader, "Bearer ")
			if rawToken != "" {
				db, err := sqlx.Open("sqlite3", constants.StudioUsersDBPath)
				if err != nil {
					http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
					return
				}

				userId, err := api_token_service.ValidateToken(db, rawToken)
				if err != nil {
					db.Close()
					if !CONFIG.Private {
						if user, ok := resolveGlobalToken(rawToken); ok {
							userBytes, _ := json.Marshal(user)
							r.Header.Set("UserData", string(userBytes))
							r.Header.Set("UserId", user.Id)
							ctx := context.WithValue(r.Context(), apiUserContextKey, userBytes)
							next.ServeHTTP(w, r.WithContext(ctx))
							return
						}
					}
					http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
					return
				}

				tx, err := db.Beginx()
				if err != nil {
					db.Close()
					http.Error(w, `{"error": "Internal server error"}`, http.StatusInternalServerError)
					return
				}

				user, err := user_service.GetUser(tx, userId)
				tx.Rollback()
				db.Close()
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

				// Inject UserData/UserId headers so downstream handlers work transparently
				userBytes, _ := json.Marshal(userInfo)
				r.Header.Set("UserData", string(userBytes))
				r.Header.Set("UserId", userInfo.Id)

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
	if userData != nil {
		if userBytes, ok := userData.([]byte); ok {
			var user UserInfo
			if err := json.Unmarshal(userBytes, &user); err == nil {
				return user, true
			}
		}
	}

	return UserInfo{}, false
}

// resolveGlobalToken validates a Bearer token against the global server
// and caches the result. Returns the user if the token is valid.
func resolveGlobalToken(token string) (UserInfo, bool) {
	// Check cache first
	tokenCacheMu.RLock()
	if entry, ok := tokenCache[token]; ok && time.Now().Before(entry.expiresAt) {
		tokenCacheMu.RUnlock()
		return entry.user, true
	}
	tokenCacheMu.RUnlock()

	// Validate against global server
	req, err := http.NewRequest("GET", constants.HOST+"/auth/authenticated", nil)
	if err != nil {
		return UserInfo{}, false
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Legacy auth: global server request failed: %v", err)
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
		Authenticated bool `json:"authenticated"`
		User          struct {
			Id        string `json:"id"`
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
			Username  string `json:"username"`
			Email     string `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &result); err != nil || !result.Authenticated {
		return UserInfo{}, false
	}

	user := UserInfo{
		Id:        result.User.Id,
		FirstName: result.User.FirstName,
		LastName:  result.User.LastName,
		Username:  result.User.Username,
		Email:     result.User.Email,
	}

	// Cache the result
	tokenCacheMu.Lock()
	tokenCache[token] = tokenCacheEntry{user: user, expiresAt: time.Now().Add(cacheTTL)}
	tokenCacheMu.Unlock()

	return user, true
}
