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
func GetStudioUsers() ([]models.StudioUserInfo, error) {
	///studio/{studio_id}/persons
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
