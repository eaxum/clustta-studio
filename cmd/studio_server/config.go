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

	if cfg.ProjectsDir == "" {
		cfg.ProjectsDir = defaultProjectsDir
	}
	if cfg.SharedProjectsDir == "" {
		cfg.SharedProjectsDir = defaultSharedProjectsDir
	}
}
