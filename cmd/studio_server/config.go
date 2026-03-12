package main

import (
	"clustta/internal/settings"
	"clustta/internal/utils"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Host              string `json:"host" envconfig:"CLUSTTA_HOST"`
	Port              string `json:"port" envconfig:"CLUSTTA_PORT"`
	ProjectsDir       string `json:"projects_dir" envconfig:"PROJECTS_DIR"`
	SharedProjectsDir string `json:"shared_projects_dir" envconfig:"SHARED_PROJECTS_DIR"`
	ServerURL         string `json:"server_url" envconfig:"CLUSTTA_SERVER_URL"`
	ServerAltURL      string `json:"server_alt_url" envconfig:"CLUSTTA_SERVER_ALT_URL"`
	ServerName        string `json:"server_name" envconfig:"CLUSTTA_SERVER_NAME"`
	StudioAPIKey      string `json:"studio_api_key" envconfig:"CLUSTTA_STUDIO_API_KEY"`
	StudioUsersDB     string `json:"studio_users_db" envconfig:"STUDIO_USERS_DB"`
	SessionDB         string `json:"session_db" envconfig:"SESSION_DB"`
	Private           bool   `json:"private" envconfig:"PRIVATE"`
	RegisteredAt      string `json:"registered_at,omitempty"`
}

var CONFIG Config = Config{}

func processError(err error) {
	fmt.Println(err)
	os.Exit(2)
}

func readFile(cfg *Config) {
	if utils.FileExists("studio_config.json") {
		f, err := os.Open("studio_config.json")
		if err != nil {
			processError(err)
		}
		defer f.Close()

		decoder := json.NewDecoder(f)
		err = decoder.Decode(cfg)
		if err != nil {
			processError(err)
		}
	} else {
		settingsPath, err := settings.GetSettingsPath()
		if err != nil {
			processError(err)
		}
		f, err := os.Open(settingsPath)
		if err != nil {
			processError(err)
		}
		defer f.Close()

		decoder := json.NewDecoder(f)
		err = decoder.Decode(cfg)
		if err != nil {
			processError(err)
		}

	}
}

func readEnv(cfg *Config) {
	err := envconfig.Process("", cfg)
	if err != nil {
		processError(err)
	}
}

func loadDefaults(cfg *Config) {
	if cfg.Host == "" {
		cfg.Host = "0.0.0.0"
	}
	if cfg.Port == "" {
		cfg.Port = "7774"
	}

	homedir, err := os.UserHomeDir()
	if err != nil {
		processError(err)
	}
	defaultProjectsDir := filepath.Join(homedir, "clustta", "projects")
	defaultSharedProjectsDir := filepath.Join(homedir, "clustta", "shared_projects")
	defaultDataDir := filepath.Join(homedir, "clustta", "data")

	// Create data directory if it doesn't exist
	if err := os.MkdirAll(defaultDataDir, os.ModePerm); err != nil {
		processError(err)
	}

	if cfg.ProjectsDir == "" {
		cfg.ProjectsDir = defaultProjectsDir
	}
	if cfg.SharedProjectsDir == "" {
		cfg.SharedProjectsDir = defaultSharedProjectsDir
	}
	if cfg.StudioUsersDB == "" {
		cfg.StudioUsersDB = filepath.Join(defaultDataDir, "studio_users.db")
	}
	if cfg.SessionDB == "" {
		cfg.SessionDB = filepath.Join(defaultDataDir, "sessions.db")
	}
}

// saveConfig writes the current config to studio_config.json
func saveConfig(cfg *Config) error {
	file, err := os.Create("studio_config.json")
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(cfg); err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	return nil
}
