package main

import (
	"clustta/internal/cryptoutil"
	"clustta/internal/integrations"
	"clustta/internal/server/studio_integration_service"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Single-tenant mirror of clustta-server's studio integration handlers.
// URL omits {studio_id}, auth uses local admin role, no entitlement check.

// studioIdSingleTenant is the constant studio_id stored for this binary's rows.
const studioIdSingleTenant = ""

// studioIntegrationPayload is the request body for save and test calls.
type studioIntegrationPayload struct {
	ApiUrl   string `json:"api_url"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Enabled  bool   `json:"enabled"`
}

// studioIntegrationView is the public response shape; never includes credentials.
type studioIntegrationView struct {
	IntegrationId   string `json:"integration_id"`
	StudioId        string `json:"studio_id"`
	ApiUrl          string `json:"api_url"`
	Email           string `json:"email"`
	Enabled         bool   `json:"enabled"`
	LastValidatedAt int64  `json:"last_validated_at"`
	LastError       string `json:"last_error"`
	Status          string `json:"status"`
	Configured      bool   `json:"configured"`
	Warning         string `json:"warning,omitempty"`
}

// normaliseApiUrl trims whitespace and trailing slashes from an api_url.
func normaliseApiUrl(apiUrl string) string {
	return strings.TrimRight(strings.TrimSpace(apiUrl), "/")
}

// insecureApiUrlWarning returns a non-blocking warning for plain-HTTP non-loopback URLs.
func insecureApiUrlWarning(apiUrl string) string {
	u, err := url.Parse(apiUrl)
	if err != nil || u == nil {
		return ""
	}
	if strings.ToLower(u.Scheme) != "http" {
		return ""
	}
	host := u.Hostname()
	if host == "" || host == "localhost" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return "This integration URL uses plain HTTP. Credentials are transmitted in cleartext on every reconnect; switch to HTTPS in production."
}

// resolveIntegrationAccess authenticates the request and returns an open DB.
// Caller must Close the returned db.
func resolveIntegrationAccess(w http.ResponseWriter, r *http.Request) (db *sqlx.DB, integrationId string, ok bool) {
	authUser, authed := getAuthUser(r)
	if !authed {
		SendErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return nil, "", false
	}
	serverUser := Users[authUser.Id]
	if serverUser.RoleName != "admin" {
		SendErrorResponse(w, "Only admins can manage integrations", http.StatusForbidden)
		return nil, "", false
	}

	integrationId = r.PathValue("integration_id")
	if integrationId == "" {
		SendErrorResponse(w, "Missing integration id", http.StatusBadRequest)
		return nil, "", false
	}
	if _, err := integrations.Get(integrationId); err != nil {
		SendErrorResponse(w, "Unknown integration", http.StatusBadRequest)
		return nil, "", false
	}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return nil, "", false
	}
	return db, integrationId, true
}

// validateKitsuCredentials performs a live login to verify credentials.
func validateKitsuCredentials(integrationId, apiUrl, email, password string) error {
	integration, err := integrations.Get(integrationId)
	if err != nil {
		return err
	}
	result, err := integration.Authenticate(map[string]string{
		"email":    email,
		"password": password,
		"api_url":  apiUrl,
	})
	if err != nil {
		return err
	}
	if !result.Success {
		return errors.New(result.Error)
	}
	return nil
}

// SaveStudioIntegrationHandler validates, encrypts and persists credentials,
// then restarts the listener.
func SaveStudioIntegrationHandler(w http.ResponseWriter, r *http.Request) {
	db, integrationId, ok := resolveIntegrationAccess(w, r)
	if !ok {
		return
	}
	defer db.Close()

	var payload studioIntegrationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	payload.ApiUrl = normaliseApiUrl(payload.ApiUrl)
	payload.Email = strings.TrimSpace(payload.Email)
	if payload.ApiUrl == "" || payload.Email == "" || payload.Password == "" {
		SendErrorResponse(w, "api_url, email and password are required", http.StatusBadRequest)
		return
	}
	masterKey, err := getMasterKey()
	if err != nil {
		SendErrorResponse(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	if err := validateKitsuCredentials(integrationId, payload.ApiUrl, payload.Email, payload.Password); err != nil {
		SendErrorResponse(w, "Could not authenticate with the integration: "+err.Error(), http.StatusBadRequest)
		return
	}

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	creds := studio_integration_service.Credentials{
		Email:    payload.Email,
		Password: payload.Password,
	}
	cfg, err := studio_integration_service.Upsert(tx, studioIdSingleTenant, integrationId, payload.ApiUrl, creds, masterKey)
	if err != nil {
		SendErrorResponse(w, "Failed to save integration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if !payload.Enabled {
		if err := studio_integration_service.SetEnabled(tx, cfg.Id, false); err != nil {
			SendErrorResponse(w, "Failed to disable integration: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := studio_integration_service.SetLastValidated(tx, cfg.Id, time.Now().Unix()); err != nil {
		log.Printf("studio_integration: SetLastValidated cfg=%s: %v", cfg.Id, err)
	}
	if err := tx.Commit(); err != nil {
		SendErrorResponse(w, "Failed to commit integration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if ListenerManager != nil {
		if payload.Enabled {
			if err := ListenerManager.Restart(r.Context(), studioIdSingleTenant, integrationId); err != nil {
				SendErrorResponse(w, "Saved but failed to start listener: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			ListenerManager.Stop(studioIdSingleTenant, integrationId)
		}
	}

	respondWithIntegrationView(w, db, integrationId)
}

// GetStudioIntegrationHandler returns the current configuration without credentials.
func GetStudioIntegrationHandler(w http.ResponseWriter, r *http.Request) {
	db, integrationId, ok := resolveIntegrationAccess(w, r)
	if !ok {
		return
	}
	defer db.Close()
	respondWithIntegrationView(w, db, integrationId)
}

// DeleteStudioIntegrationHandler stops the listener and removes the stored config.
func DeleteStudioIntegrationHandler(w http.ResponseWriter, r *http.Request) {
	db, integrationId, ok := resolveIntegrationAccess(w, r)
	if !ok {
		return
	}
	defer db.Close()

	if ListenerManager != nil {
		ListenerManager.Stop(studioIdSingleTenant, integrationId)
	}

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	if err := studio_integration_service.Delete(tx, studioIdSingleTenant, integrationId); err != nil {
		SendErrorResponse(w, "Failed to delete integration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		SendErrorResponse(w, "Failed to commit deletion: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TestStudioIntegrationHandler revalidates supplied or stored credentials
// without persisting changes.
func TestStudioIntegrationHandler(w http.ResponseWriter, r *http.Request) {
	db, integrationId, ok := resolveIntegrationAccess(w, r)
	if !ok {
		return
	}
	defer db.Close()

	// Optional payload; empty body re-tests stored credentials. Partial
	// payloads are rejected to avoid testing a new url against stored creds.
	var payload studioIntegrationPayload
	_ = json.NewDecoder(r.Body).Decode(&payload)
	payload.ApiUrl = normaliseApiUrl(payload.ApiUrl)
	payload.Email = strings.TrimSpace(payload.Email)

	supplied := 0
	if payload.ApiUrl != "" {
		supplied++
	}
	if payload.Email != "" {
		supplied++
	}
	if payload.Password != "" {
		supplied++
	}
	if supplied != 0 && supplied != 3 {
		SendErrorResponse(w, "Provide all of api_url, email and password to test new credentials, or send an empty body to re-test stored credentials.", http.StatusBadRequest)
		return
	}

	apiUrl := payload.ApiUrl
	email := payload.Email
	password := payload.Password
	if supplied == 0 {
		masterKey, err := getMasterKey()
		if err != nil {
			SendErrorResponse(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		tx, err := db.Beginx()
		if err != nil {
			SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		cfg, err := studio_integration_service.Get(tx, studioIdSingleTenant, integrationId)
		tx.Rollback()
		if err != nil {
			SendErrorResponse(w, "Integration not configured", http.StatusNotFound)
			return
		}
		creds, err := studio_integration_service.DecryptCredentials(cfg, masterKey)
		if err != nil {
			SendErrorResponse(w, "Failed to read stored credentials", http.StatusInternalServerError)
			return
		}
		apiUrl = cfg.ApiUrl
		email = creds.Email
		password = creds.Password
	}

	if err := validateKitsuCredentials(integrationId, apiUrl, email, password); err != nil {
		SendErrorResponse(w, err.Error(), http.StatusBadRequest)
		return
	}
	tx, err := db.Beginx()
	if err == nil {
		if cfg, getErr := studio_integration_service.Get(tx, studioIdSingleTenant, integrationId); getErr == nil {
			_ = studio_integration_service.SetLastValidated(tx, cfg.Id, time.Now().Unix())
			_ = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"warning": insecureApiUrlWarning(apiUrl),
	})
}

// SetStudioIntegrationEnabledHandler toggles enabled without requiring credentials
// and starts or stops the listener accordingly.
func SetStudioIntegrationEnabledHandler(w http.ResponseWriter, r *http.Request) {
	db, integrationId, ok := resolveIntegrationAccess(w, r)
	if !ok {
		return
	}
	defer db.Close()

	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		SendErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	cfg, err := studio_integration_service.Get(tx, studioIdSingleTenant, integrationId)
	if err != nil {
		tx.Rollback()
		if errors.Is(err, studio_integration_service.ErrNotFound) {
			SendErrorResponse(w, "Integration not configured", http.StatusNotFound)
			return
		}
		SendErrorResponse(w, "Failed to load integration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := studio_integration_service.SetEnabled(tx, cfg.Id, payload.Enabled); err != nil {
		tx.Rollback()
		SendErrorResponse(w, "Failed to update integration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tx.Commit(); err != nil {
		SendErrorResponse(w, "Failed to commit update: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if ListenerManager != nil {
		if payload.Enabled {
			if err := ListenerManager.Restart(r.Context(), studioIdSingleTenant, integrationId); err != nil {
				SendErrorResponse(w, "Updated but failed to start listener: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			ListenerManager.Stop(studioIdSingleTenant, integrationId)
		}
	}

	respondWithIntegrationView(w, db, integrationId)
}

// respondWithIntegrationView writes the public view or an unconfigured placeholder.
func respondWithIntegrationView(w http.ResponseWriter, db *sqlx.DB, integrationId string) {
	tx, err := db.Beginx()
	if err != nil {
		SendErrorResponse(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()
	cfg, err := studio_integration_service.Get(tx, studioIdSingleTenant, integrationId)
	if err != nil {
		if errors.Is(err, studio_integration_service.ErrNotFound) {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(studioIntegrationView{
				IntegrationId: integrationId,
				StudioId:      studioIdSingleTenant,
				Configured:    false,
			})
			return
		}
		SendErrorResponse(w, err.Error(), http.StatusInternalServerError)
		return
	}
	masterKey, err := getMasterKey()
	if err != nil {
		SendErrorResponse(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	creds, err := studio_integration_service.DecryptCredentials(cfg, masterKey)
	if err != nil {
		SendErrorResponse(w, "Failed to read stored credentials", http.StatusInternalServerError)
		return
	}
	var lastValidated int64
	if cfg.LastValidatedAt.Valid {
		lastValidated = cfg.LastValidatedAt.Int64
	}
	view := studioIntegrationView{
		IntegrationId:   cfg.IntegrationId,
		StudioId:        cfg.StudioId,
		ApiUrl:          cfg.ApiUrl,
		Email:           creds.Email,
		Enabled:         cfg.Enabled,
		LastValidatedAt: lastValidated,
		LastError:       cfg.LastError,
		Status:          "stopped",
		Configured:      true,
		Warning:         insecureApiUrlWarning(cfg.ApiUrl),
	}
	if ListenerManager != nil {
		view.Status = ListenerManager.Status(studioIdSingleTenant, integrationId)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(view); err != nil {
		SendErrorResponse(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// getMasterKey decodes the integration master key from CONFIG.
// Returns an error when the key is unset or invalid.
func getMasterKey() ([]byte, error) {
	if CONFIG.IntegrationSecretKey == "" {
		return nil, errors.New("integration storage is not configured on this server")
	}
	key, err := cryptoutil.DecodeKey(CONFIG.IntegrationSecretKey)
	if err != nil {
		return nil, errors.New("server integration key is invalid")
	}
	return key, nil
}
