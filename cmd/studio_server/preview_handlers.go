package main

import (
	"clustta/internal/repository"
	"clustta/internal/repository/models"
	"clustta/internal/repository/repositorypb"
	"clustta/internal/utils"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/DataDog/zstd"
	"google.golang.org/protobuf/proto"
)

func SendErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	response := map[string]interface{}{
		"message": message,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("{\"message\": \"Failed to encode response\"}"))
	}
}

func GetPreviewsHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	type previewsStruct struct {
		Previews []string `json:"previews"`
	}
	var data previewsStruct
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	previews := []models.Preview{}
	for _, previewHash := range data.Previews {
		preview, err := repository.GetPreview(tx, previewHash)
		if err != nil {
			log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
			return
		}
		previews = append(previews, preview)
	}

	pbPreviews := repository.ToPbPreviews(previews)
	pbPreviewsList := &repositorypb.Previews{Previews: pbPreviews}
	pbPreviewsListByte, err := proto.Marshal(pbPreviewsList)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	compressedData, err := zstd.CompressLevel(nil, pbPreviewsListByte, 3)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}

	w.Write(compressedData)
}

func PostPreviewsHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}

	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	previewsData, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}

	decompressedData, err := zstd.Decompress(nil, previewsData)
	if err != nil {
		http.Error(w, "Failed to decompress data", 400)
		return
	}
	if len(decompressedData) > 50<<20 {
		http.Error(w, "Decompressed data exceeds size limit", 413)
		return
	}

	previewList := repositorypb.Previews{}
	err = proto.Unmarshal(decompressedData, &previewList)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	previews := repository.FromPbPreviews(previewList.Previews)

	err = repository.AddPreviews(tx, previews)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	err = tx.Commit()
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
}

func PreviewsExistHandler(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
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
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()
	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	decoder := json.NewDecoder(r.Body)
	var data []string
	err = decoder.Decode(&data)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		// errMessage := ErrorStruct{
		// 	Message: err.Error(),
		// }
		// objJson, _ := json.Marshal(errMessage)
		// w.Write(objJson)
		return
	}

	missingPreviews := []string{}
	for _, previewHash := range data {
		if repository.PreviewExists(previewHash, tx) {
			continue
		}
		missingPreviews = append(missingPreviews, previewHash)
	}

	objJson, err := json.Marshal(missingPreviews)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	w.Write(objJson)
}

func GetProjectPreview(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	projectPath, pathErr := safeProjectPath(CONFIG.ProjectsDir, project)
	if pathErr != nil {
		http.Error(w, "Invalid project name", http.StatusBadRequest)
		return
	}
	if !utils.FileExists(projectPath) {
		http.Error(w, "Project Not Found", 400)
		return
	}
	dbConn, err := utils.OpenDb(projectPath)
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer dbConn.Close()

	tx, err := dbConn.Beginx()
	if err != nil {
		log.Printf("Request error: %v", err)
	http.Error(w, "Internal server error", 400)
		return
	}
	defer tx.Rollback()

	projectPreview, err := repository.GetProjectPreview(tx)
	if err != nil {
		if err.Error() == "no preview" {
			w.Write([]byte{})
			return
		}
		log.Printf("Error getting preview: %v", err)
	SendErrorResponse(w, "Error getting preview", http.StatusInternalServerError)
		return
	}
	w.Write(projectPreview.Preview)

}
