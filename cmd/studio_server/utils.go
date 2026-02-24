package main

import (
	"bytes"
	"clustta/internal/constants"
	"clustta/internal/server/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// NetworkError represents a network connectivity error (can be retried later)
type NetworkError struct {
	Err error
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("network error: %v", e.Err)
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}

// APIError represents an API/server error (credentials invalid, studio not found, etc.)
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error (status %d): %s", e.StatusCode, e.Message)
}

// IsNetworkError checks if an error is a network connectivity error
func IsNetworkError(err error) bool {
	var netErr *NetworkError
	return errors.As(err, &netErr)
}

func UpdateStudioUrl() error {
	url := constants.HOST + "/studio/" + CONFIG.ServerName + "/url"

	data := []byte(fmt.Sprintf(`{"key": "%s", "url": "%s", "alt_url": "%s", "port": "%s"}`, CONFIG.StudioAPIKey, CONFIG.ServerURL, CONFIG.ServerAltURL, CONFIG.Port))

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	response, err := client.Do(req)
	if err != nil {
		// Check if it's a network-related error
		var netErr net.Error
		if errors.As(err, &netErr) || isConnectionError(err) {
			return &NetworkError{Err: err}
		}
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		return &APIError{StatusCode: response.StatusCode, Message: string(body)}
	}

	return nil
}

// isConnectionError checks for common connection errors
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Common network error patterns
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "dial tcp")
}

// GetStudioUsers returns users from local DB in private mode, or from global server otherwise
func GetStudioUsers() ([]models.StudioUserInfo, error) {
	if CONFIG.Private {
		return getLocalStudioUsers()
	}
	return getRemoteStudioUsers()
}

// getLocalStudioUsers fetches users from the local studio_users.db with role info.
// Follows the same pattern as the global server: query users, load roles, build struct manually.
func getLocalStudioUsers() ([]models.StudioUserInfo, error) {
	studioUserInfos := []models.StudioUserInfo{}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		return studioUserInfos, fmt.Errorf("failed to open local users db: %w", err)
	}
	defer db.Close()

	// Query active users
	var users []models.User
	err = db.Select(&users, "SELECT * FROM user WHERE active = 1")
	if err != nil {
		return studioUserInfos, fmt.Errorf("failed to query users: %w", err)
	}

	// Load all roles into a map
	var roles []models.Role
	err = db.Select(&roles, "SELECT * FROM role")
	if err != nil {
		return studioUserInfos, fmt.Errorf("failed to query roles: %w", err)
	}
	roleMap := map[string]models.Role{}
	for _, role := range roles {
		roleMap[role.Id] = role
	}

	// Build StudioUserInfo for each user
	for _, user := range users {
		roleName := "user"
		roleId := ""
		if user.RoleId.Valid && user.RoleId.String != "" {
			roleId = user.RoleId.String
			if role, ok := roleMap[roleId]; ok {
				roleName = role.Name
			}
		}

		studioUserInfo := models.StudioUserInfo{
			Id:         user.Id,
			FirstName:  user.FirstName,
			LastName:   user.LastName,
			UserName:   user.UserName,
			Email:      user.Email,
			Active:     user.Active,
			RoleName:   roleName,
			StudioName: CONFIG.ServerName,
			RoleId:     roleId,
			Photo:      user.Photo,
		}
		studioUserInfos = append(studioUserInfos, studioUserInfo)
	}

	return studioUserInfos, nil
}

// getRemoteStudioUsers fetches users from the global Clustta server
func getRemoteStudioUsers() ([]models.StudioUserInfo, error) {
	users := []models.StudioUserInfo{}
	url := constants.HOST + "/studio/" + CONFIG.ServerName + ":" + CONFIG.StudioAPIKey + "/persons"

	data := []byte(fmt.Sprintf(`{"key": "%s", "url": "%s", "port": "%s"}`, CONFIG.StudioAPIKey, CONFIG.ServerURL, CONFIG.Port))

	req, err := http.NewRequest(http.MethodGet, url, bytes.NewBuffer(data))
	if err != nil {
		return users, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return users, err
	}
	defer response.Body.Close()

	if response.StatusCode == 200 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return users, err
		}
		err = json.Unmarshal(body, &users)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal response body: %v", err)
		}
		return users, nil
	}
	_, err = io.ReadAll(response.Body)
	if err != nil {
		return users, err
	}
	return users, err
}
