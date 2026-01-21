package main

import (
	"clustta/internal/repository"
	"clustta/internal/server/models"
	"clustta/internal/settings"
	"clustta/internal/utils"
	"database/sql"
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

	// Initialize studio users database early (needed for private mode user lookup)
	if err := initStudioUsersDB(CONFIG.StudioUsersDB); err != nil {
		log.Fatalf("Failed to initialize studio users database: %v", err)
	}

	// Only register with global server if not in private mode
	var err error
	if !CONFIG.Private {
		err = UpdateStudioUrl()
		if err != nil {
			println(err.Error())
			return
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
	sessionDb, err := initSessionDB(CONFIG.SessionDB)
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

// initStudioUsersDB creates the studio users database and schema
func initStudioUsersDB(dbPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create users table (must match base_service expectations including mtime)
	schema := `
	CREATE TABLE IF NOT EXISTS user (
		id TEXT PRIMARY KEY,
		mtime INTEGER NOT NULL DEFAULT 0,
		first_name TEXT NOT NULL,
		last_name TEXT NOT NULL,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE,
		email TEXT NOT NULL UNIQUE COLLATE NOCASE,
		password TEXT NOT NULL,
		last_presence DATETIME DEFAULT CURRENT_TIMESTAMP,
		login_failed_attempts INTEGER NOT NULL DEFAULT 0,
		last_login_failed DATETIME DEFAULT NULL,
		totp_enabled BOOLEAN NOT NULL DEFAULT 0,
		totp_secret TEXT DEFAULT NULL,
		email_otp_enabled BOOLEAN NOT NULL DEFAULT 0,
		email_otp_secret TEXT DEFAULT NULL,
		photo BLOB,
		has_avatar BOOLEAN NOT NULL DEFAULT 0,
		added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		active BOOLEAN DEFAULT 1,
		is_deleted BOOLEAN DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_user_email ON user(email);
	CREATE INDEX IF NOT EXISTS idx_user_username ON user(username);
	`

	_, err = db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	log.Printf("Studio users database initialized: %s", dbPath)
	return nil
}

// initSessionDB creates the session database and returns the connection
func initSessionDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create sessions table (required by scs/sqlite3store)
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		token TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expiry REAL NOT NULL
	);
	CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry);
	`

	_, err = db.Exec(schema)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	log.Printf("Session database initialized: %s", dbPath)
	return db, nil
}
