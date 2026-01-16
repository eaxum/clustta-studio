package main

import (
	"clustta/internal/auth_service"
	"clustta/internal/chunk_service"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"
	"clustta/internal/repository/sync_service"
	"clustta/internal/utils"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DataDog/zstd"
	"google.golang.org/protobuf/proto"
)

type ErrorStruct struct {
	Message string `json:"error"`
}

type dataStruct struct {
	UserId string `json:"user_id"`
}
type PingResponse struct {
	Status string `json:"status"`
}

type StudioKeyResponse struct {
	StudioKey string `json:"studio_key"`
}

func PingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := PingResponse{Status: "active"}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

type VersionResponse struct {
	Version string `json:"version"`
}

func VersionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := VersionResponse{Version: Version}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func GetStudioKeyHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := StudioKeyResponse{StudioKey: CONFIG.StudioAPIKey}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func PostProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Clustta-Agent") != "Clustta/0.2" {
		http.Error(w, "Invalid Client", 500)
		return
	}

	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")
	// w.Write([]byte(projectPath))
	// clusttaServerDB := filepath.Join(CONFIG.DataDir, "clustta_studio.db")

	// db, err := sqlx.Open("sqlite3", clusttaServerDB)
	// if err != nil {
	// 	http.Error(w, err.Error(), 400)
	// 	return
	// }
	// defer db.Close()

	// tx, err := db.Beginx()
	// if err != nil {
	// 	http.Error(w, err.Error(), 400)
	// 	return
	// }
	// defer tx.Rollback()

	if utils.FileExists(projectPath) {
		http.Error(w, "Project Already Exist", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to create a project", 400)
		return
	}
	user = auth_service.User{
		Id:        user.Id,
		Email:     serverUser.Email,
		Username:  serverUser.UserName,
		FirstName: serverUser.FirstName,
		LastName:  serverUser.LastName,
	}

	projectInfo, err := repository.CreateProject(projectPath, "", "", "No Template", user)
	if err != nil {
		if utils.FileExists(projectPath) {
			journal := projectPath + "-journal"
			err := os.Remove(projectPath)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if utils.FileExists(journal) {
				err = os.Remove(journal)
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
			}

		}
		http.Error(w, err.Error(), 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func RenameProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	var data map[string]string
	json.NewDecoder(r.Body).Decode(&data)
	newProjectName, ok := data["name"]
	if !ok || newProjectName == "" {
		http.Error(w, "name is required", 400)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.RenameProject(projectPath, "", newProjectName, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	newProjectPath := filepath.Join(filepath.Dir(projectPath), newProjectName+".clst")

	projectInfo, err := repository.GetProjectInfo(newProjectPath, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func ToggleProjectCloseHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.ToggleCloseProject(projectPath, "", user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func SetProjectIconHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	var data map[string]string
	json.NewDecoder(r.Body).Decode(&data)
	newProjectIcon, ok := data["icon"]
	if !ok || newProjectIcon == "" {
		http.Error(w, "icon is required", 400)
		return
	}

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.SetIcon(projectPath, "", newProjectIcon, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func SetProjectIgnoreListHandler(
	w http.ResponseWriter, r *http.Request) {
	projectName := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, projectName+".clst")

	var ignoreList []string
	json.NewDecoder(r.Body).Decode(&ignoreList)

	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	serverUser := Users[user.Id]
	if serverUser.RoleName != "admin" {
		http.Error(w, "You have no permission to rename a project", 400)
		return
	}

	err = repository.SetIgnoreList(projectPath, "", ignoreList, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	projectInfo, err := repository.GetProjectInfo(projectPath, user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	objJson, _ := json.Marshal(projectInfo)
	w.Write(objJson)
}

func GetProjectHandler(
	w http.ResponseWriter, r *http.Request) {
	projectFolder := CONFIG.ProjectsDir

	projectPath := filepath.Join(projectFolder, r.PathValue("project")+".clst")
	project := repository.ProjectInfo{}

	//check if project exists
	if !utils.FileExists(projectPath) {
		http.Error(w, "project does not exist", 500)
		return
	}

	UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	userInProject, err := repository.UserInProject(projectPath, UserId)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if userInProject {
		projectInfo, err := repository.GetProjectInfo(projectPath, user)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		project = projectInfo
	} else {
		http.Error(w, "user not in project", 400)
		return
	}
	objJson, _ := json.Marshal(project)
	w.Write(objJson)
}

func GetProjectSyncTokenHandler(
	w http.ResponseWriter, r *http.Request) {
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, r.PathValue("project")+".clst")

	UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")

	user := auth_service.User{}
	err := json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	userInProject, err := repository.UserInProject(projectPath, UserId)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if userInProject {
		db, err := utils.OpenDb(projectPath)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer db.Close()
		tx, err := db.Beginx()
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		defer tx.Rollback()

		syncToken, err := utils.GetProjectSyncToken(tx)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		// jsonStr := map[string]string{"sync_token": syncToken}
		// objJson, _ := json.Marshal(jsonStr)

		byteStr := []byte(syncToken)
		w.Write(byteStr)
	} else {
		http.Error(w, "user not in project", 400)
		return
	}
}

func GetProjectsHandler(
	w http.ResponseWriter, r *http.Request) {
	projectFolder := CONFIG.ProjectsDir

	extension := "clst"
	projects := []repository.ProjectInfo{}

	// Read the directory
	entries, err := os.ReadDir(projectFolder)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// UserId := r.Header.Get("UserId")
	UserData := r.Header.Get("UserData")
	user := auth_service.User{}
	err = json.Unmarshal([]byte(UserData), &user)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	println("UserId", user.Id)

	// Iterate over the directory entries
	for _, entry := range entries {
		// Check if the entry is a file and has the specified extension
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), extension) {
			projectPath := filepath.Join(projectFolder, entry.Name())

			fileInfo, err := entry.Info()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if fileInfo.Size() == 0 {
				os.Remove(projectPath)
			}

			valid, err := repository.VerifyProjectIntegrity(projectPath)
			if !valid || err != nil {
				continue
			}

			userInProject, err := repository.UserInProject(projectPath, user.Id)
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if userInProject {
				projectInfo, err := repository.GetProjectInfo(projectPath, user)
				if err != nil {
					http.Error(w, err.Error(), 400)
					return
				}
				projects = append(projects, projectInfo)
			}

		}
	}

	// projectsData := map[string]interface{}{
	// 	"projects": projects,
	// }
	objJson, _ := json.Marshal(projects)
	w.Write(objJson)
}

func GetDataHandler(
	w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	// w.Write([]byte(projectPath))
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	db, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data dataStruct
	err = decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	userData, err := sync_service.LoadUserDataPb(tx, data.UserId)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	compressedData, err := zstd.CompressLevel(nil, userData, 3)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// dbConn, err := utils.OpenDb( projectPath)
	// if err != nil {
	// 	panic(err)
	// }
	// tx, err := dbConn.Beginx()
	// if err != nil {
	// 	return err
	// }

	// users, err := repository.GetUsers(tx)
	// if err != nil {
	// 	return err
	// }
	// tx.Commit()

	// objJson, err := json.Marshal(userData)
	// if err != nil {
	// 	http.Error(w, err.Error(), 400)
	// 	return
	// }
	_, err = w.Write(compressedData)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

}

func PostDataHandler(
	w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Clustta-Agent") != "Clustta/0.2" {
		http.Error(w, "Invalid Client", 500)
		return
	}
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer db.Close()
	tx, err := db.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	decompressedData, err := zstd.Decompress(nil, body)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	userDataPb := repositorypb.ProjectData{}
	err = proto.Unmarshal(decompressedData, &userDataPb)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	requestData := sync_service.ProjectData{
		ProjectPreview:  userDataPb.ProjectPreview,
		EntityTypes:     repository.FromPbEntityTypes(userDataPb.EntityTypes),
		Entities:        repository.FromPbEntities(userDataPb.Entities),
		EntityAssignees: repository.FromPbEntityAssignees(userDataPb.EntityAssignees),

		TaskTypes:          repository.FromPbTaskTypes(userDataPb.TaskTypes),
		Tasks:              repository.FromPbTasks(userDataPb.Tasks),
		TasksCheckpoints:   repository.FromPbCheckpoints(userDataPb.TasksCheckpoints),
		TaskDependencies:   repository.FromPbTaskDependencies(userDataPb.TaskDependencies),
		EntityDependencies: repository.FromPbEntityDependencies(userDataPb.EntityDependencies),

		Statuses:        repository.FromPbStatuses(userDataPb.Statuses),
		DependencyTypes: repository.FromPbDependencyTypes(userDataPb.DependencyTypes),

		Users: repository.FromPbUsers(userDataPb.Users),
		Roles: repository.FromPbRoles(userDataPb.Roles),

		Templates: repository.FromPbTemplates(userDataPb.Templates),

		Workflows:        repository.FromPbWorkflows(userDataPb.Workflows),
		WorkflowLinks:    repository.FromPbWorkflowLinks(userDataPb.WorkflowLinks),
		WorkflowEntities: repository.FromPbWorkflowEntities(userDataPb.WorkflowEntities),
		WorkflowTasks:    repository.FromPbWorkflowTasks(userDataPb.WorkflowTasks),

		Tags:      repository.FromPbTags(userDataPb.Tags),
		TasksTags: repository.FromPbTaskTags(userDataPb.TasksTags),

		Tombs: repository.FromPbTombs(userDataPb.Tomb),
	}

	conflictResult, err := sync_service.CheckForConflicts(tx, requestData)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	if !conflictResult.Success {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(conflictResult)
		return
	}

	err = sync_service.WriteProjectData(tx, requestData, true)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	err = repository.UpdateUsersPhoto(tx)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	err = repository.AddItemsToTomb(tx, requestData.Tombs)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	newSyncToken := utils.GenerateToken()
	err = utils.SetProjectSyncToken(tx, newSyncToken)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	err = tx.Commit()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
}

func GetChunksHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	// w.Write([]byte(projectPath))
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	type chunksStruct struct {
		Chunks []string `json:"chunks"`
	}
	var data chunksStruct
	err = decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	chunks := []chunk_service.Chunk{}
	for _, chunkHash := range data.Chunks {
		var chunkData []byte
		err = tx.Get(&chunkData, "SELECT data FROM chunk WHERE hash = ?", chunkHash)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		chunk := chunk_service.Chunk{
			Hash: chunkHash,
			Data: chunkData,
		}
		chunks = append(chunks, chunk)
	}

	encodedChunks, err := chunk_service.EncodeChunks(chunks)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Write(encodedChunks)
}

func StreamChunksHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	type chunksStruct struct {
		Chunks []string `json:"chunks"`
	}
	var data chunksStruct
	err = decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// Enable streaming response
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Transfer-Encoding", "chunked")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, chunkHash := range data.Chunks {
		var chunkData []byte
		err = tx.Get(&chunkData, "SELECT data FROM chunk WHERE hash = ?", chunkHash)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		chunk := chunk_service.Chunk{
			Hash: chunkHash,
			Data: chunkData,
		}
		encodedChunk, err := chunk_service.EncodeChunk(chunk)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		// Send chunk data to client
		_, _ = w.Write(encodedChunk)
		flusher.Flush() // Flush to stream each chunk immediately
	}
}

func PostChunksHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	// w.Write([]byte(projectPath))
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	chunks, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	failedChunks, err := chunk_service.WriteChunks(projectPath, chunks)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	data := map[string]interface{}{
		"failed_chunks": failedChunks,
	}
	objJson, err := json.Marshal(data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Write(objJson)
}

func ChunksMissingHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	// w.Write([]byte(projectPath))
	if !utils.FileExists(projectPath) {
		// errMessage := ErrorStruct{
		// 	Message: "Project Not Found",
		// }
		// objJson, _ := json.Marshal(errMessage)
		// w.Write(objJson)
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data []string
	err = decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		// errMessage := ErrorStruct{
		// 	Message: err.Error(),
		// }
		// objJson, _ := json.Marshal(errMessage)
		// w.Write(objJson)
		return
	}

	missingChunks := []string{}
	seenChunks := make(map[string]bool)
	for _, chunkHash := range data {
		if chunk_service.ChunkExists(chunkHash, tx, seenChunks) {
			continue
		}
		missingChunks = append(missingChunks, chunkHash)
	}

	objJson, err := json.Marshal(missingChunks)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Write(objJson)
}

func GetChunksInfoHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectFolder := CONFIG.ProjectsDir
	projectPath := filepath.Join(projectFolder, project+".clst")
	// w.Write([]byte(projectPath))
	if !utils.FileExists(projectPath) {
		// errMessage := ErrorStruct{
		// 	Message: "Project Not Found",
		// }
		// objJson, _ := json.Marshal(errMessage)
		// w.Write(objJson)
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data []string
	err = decoder.Decode(&data)
	if err != nil {
		http.Error(w, err.Error(), 400)
		// errMessage := ErrorStruct{
		// 	Message: err.Error(),
		// }
		// objJson, _ := json.Marshal(errMessage)
		// w.Write(objJson)
		return
	}

	chunksInfo := []chunk_service.ChunkInfo{}
	// seenChunks := make(map[string]bool)
	for _, chunkHash := range data {
		chunkInfo, err := chunk_service.GetChunkInfo(tx, chunkHash)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		chunksInfo = append(chunksInfo, chunkInfo)
	}

	objJson, err := json.Marshal(chunksInfo)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	w.Write(objJson)
}
