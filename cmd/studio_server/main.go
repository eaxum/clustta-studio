package main

import (
	"clustta/internal/repository"
	"clustta/internal/server/models"
	"clustta/internal/settings"
	"clustta/internal/utils"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

	if serverType == "studio" && CONFIG.StudioAPIKey == "" {
		println("must provide studio api key")
		return
	}
	if serverType == "studio" && CONFIG.ServerName == "" {
		println("must provide studio name")
		return
	}

	loadDefaults(&CONFIG)

	err := UpdateStudioUrl()
	if err != nil {
		println(err.Error())
		return
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
	addr := fmt.Sprintf("%s:%s", CONFIG.Host, CONFIG.Port)
	server := NewAPIServer(addr)
	err = server.Run()
	println(err.Error())
}
