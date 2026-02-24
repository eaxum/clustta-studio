package main

import (
	"clustta/internal/constants"
	"clustta/internal/repository"
	"clustta/internal/server/models"
	"clustta/internal/server/session_service"
	"clustta/internal/settings"
	"clustta/internal/studio_users_service"
	"clustta/internal/utils"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Version is set at build time via -ldflags "-X main.Version=x.x.x"
var Version = "dev"

var Users = map[string]models.StudioUserInfo{}

func GetUsers() error {
	users, err := GetStudioUsers()
	if err != nil {
		println(err.Error())
		return err
	}
	for _, user := range users {
		Users[user.Id] = user
	}
	return nil
}
func main() {
	//get first argument
	if len(os.Args) < 2 {
		println("must provide studio or personal argument")
		return
	}

	serverType := os.Args[1]
	if serverType != "studio" && serverType != "personal" {
		println("must provide studio or personal argument")
		return
	}

	if err := settings.InitializeServer(); err != nil {
		log.Fatalf("Failed to initialize server: %v", err)
	}

	if serverType == "studio" {
		if !utils.FileExists("studio_config.json") {
			loadDefaults(&CONFIG)

			file, err := os.Create("studio_config.json")
			if err != nil {
				fmt.Println("Error creating file:", err)
				return
			}
			defer file.Close()

			// Encode the struct to JSON and write to the file
			encoder := json.NewEncoder(file)
			encoder.SetIndent("", "  ") // Optional: Pretty-print with indentation
			if err := encoder.Encode(CONFIG); err != nil {
				fmt.Println("Error encoding JSON to file:", err)
				return
			}

			fmt.Println("Default settings written to studio_config.json")
		}
	}

	if settings.IsServer() {
		log.Println("Running as server")
	}

	readFile(&CONFIG)
	readEnv(&CONFIG)

	// Private mode doesn't require API key or server name (no global server connection)
	if serverType == "studio" && !CONFIG.Private {
		if CONFIG.StudioAPIKey == "" {
			println("must provide studio api key (or set PRIVATE=true for private mode)")
			return
		}
		if CONFIG.ServerName == "" {
			println("must provide studio name (or set PRIVATE=true for private mode)")
			return
		}
	}

	loadDefaults(&CONFIG)

	// Set private mode globals for internal packages
	constants.PrivateMode = CONFIG.Private
	constants.StudioUsersDBPath = CONFIG.StudioUsersDB

	// Initialize studio users database early (needed for private mode user lookup)
	if err := studio_users_service.InitDB(CONFIG.StudioUsersDB); err != nil {
		log.Fatalf("Failed to initialize studio users database: %v", err)
	}

	// Only register with global server if not in private mode
	var err error
	if !CONFIG.Private {
		err = UpdateStudioUrl()
		if err != nil {
			if IsNetworkError(err) && CONFIG.RegisteredAt != "" {
				// Already registered, network temporarily unavailable - continue with warning
				log.Printf("Warning: Could not connect to global server (network error), but studio is already registered. Continuing in offline mode...")
			} else if IsNetworkError(err) {
				// First-time registration requires network
				log.Fatalf("Failed to register studio: %v (network connectivity required for first-time registration)", err)
			} else {
				// API error (invalid credentials, studio not found, etc.) - always fail
				log.Fatalf("Failed to register studio: %v", err)
			}
		} else {
			// Success - save registration timestamp if not already set
			if CONFIG.RegisteredAt == "" {
				CONFIG.RegisteredAt = time.Now().UTC().Format(time.RFC3339)
				if saveErr := saveConfig(&CONFIG); saveErr != nil {
					log.Printf("Warning: Could not save registration timestamp: %v", saveErr)
				} else {
					log.Println("Studio registered successfully")
				}
			}
		}
	} else {
		log.Println("Running in PRIVATE mode - no connection to global server")
	}

	err = GetUsers()
	if err != nil {
		println(err.Error())
		return
	}

	// Read the directory
	projectFolder := CONFIG.ProjectsDir
	extension := "clst"

	// Ensure projects directory exists
	if err := os.MkdirAll(projectFolder, os.ModePerm); err != nil {
		println("Failed to create projects directory:", err.Error())
		return
	}

	entries, err := os.ReadDir(projectFolder)
	if err != nil {
		println(err.Error())
		return
	}

	// Iterate over the directory entries
	for _, entry := range entries {
		// Check if the entry is a file and has the specified extension
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), extension) {
			projectPath := filepath.Join(projectFolder, entry.Name())

			err := repository.UpdateProject(projectPath)
			if err != nil {
				println(err.Error())
				return
			}
		}
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			if err := GetUsers(); err != nil {
				log.Printf("Error running GetUsers: %v", err)
			}
		}
	}()

	var newConfig Config = Config{}
	// Only run URL update goroutine if not in private mode
	if !CONFIG.Private {
		go func() {
			for {
				time.Sleep(5 * time.Second)
				readFile(&newConfig)
				readEnv(&newConfig)
				if newConfig.ServerURL != CONFIG.ServerURL || newConfig.ServerAltURL != CONFIG.ServerAltURL {
					CONFIG.ServerAltURL = newConfig.ServerAltURL
					CONFIG.ServerURL = newConfig.ServerURL
					err = UpdateStudioUrl()
					if err != nil {
						log.Printf("Error Updating ServerUrl: %v", err)
					}
					log.Printf("Server URL updated to %s", CONFIG.ServerURL)
					log.Printf("Server Alt URL updated to %s", CONFIG.ServerAltURL)
				}
			}
		}()
	}

	// Initialize session database
	sessionDb, err := session_service.OpenDB(CONFIG.SessionDB)
	if err != nil {
		log.Fatalf("Failed to initialize session database: %v", err)
	}
	defer sessionDb.Close()

	// Initialize session manager
	InitSessionManager(sessionDb)

	addr := fmt.Sprintf("%s:%s", CONFIG.Host, CONFIG.Port)
	server := NewAPIServer(addr)
	err = server.Run()
	println(err.Error())
}
