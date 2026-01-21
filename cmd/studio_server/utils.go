package main

import (
	"bytes"
	"clustta/internal/constants"
	"clustta/internal/server/models"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/jmoiron/sqlx"
)

func UpdateStudioUrl() error {
	url := constants.HOST + "/studio/" + CONFIG.ServerName + "/url"

	data := []byte(fmt.Sprintf(`{"key": "%s", "url": "%s", "alt_url": "%s", "port": "%s"}`, CONFIG.StudioAPIKey, CONFIG.ServerURL, CONFIG.ServerAltURL, CONFIG.Port))

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(data))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		body, err := io.ReadAll(response.Body)
		if err != nil {
			return err
		}
		return errors.New(string(body))
	}

	return nil
}

// GetStudioUsers returns users from local DB in private mode, or from global server otherwise
func GetStudioUsers() ([]models.StudioUserInfo, error) {
	if CONFIG.Private {
		return getLocalStudioUsers()
	}
	return getRemoteStudioUsers()
}

// getLocalStudioUsers fetches users from the local studio_users.db
func getLocalStudioUsers() ([]models.StudioUserInfo, error) {
	users := []models.StudioUserInfo{}

	db, err := sqlx.Open("sqlite3", CONFIG.StudioUsersDB)
	if err != nil {
		return users, fmt.Errorf("failed to open local users db: %w", err)
	}
	defer db.Close()

	query := `
		SELECT id, first_name, last_name, username, email, active
		FROM user
		WHERE active = 1
	`
	rows, err := db.Queryx(query)
	if err != nil {
		return users, fmt.Errorf("failed to query local users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var user models.StudioUserInfo
		err := rows.StructScan(&user)
		if err != nil {
			continue
		}
		// Set default values for fields not in local DB
		user.RoleName = "member"
		user.StudioName = CONFIG.ServerName
		users = append(users, user)
	}

	return users, nil
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
