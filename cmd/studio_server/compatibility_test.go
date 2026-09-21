package main

import (
	"clustta/internal/compatibility"
	"clustta/internal/repository/migrations"
	"clustta/internal/utils"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestCompatibilityMatchesMigrationTarget(t *testing.T) {
	if compatibility.Schema != migrations.LatestVersion {
		t.Fatalf("contract schema %s does not match migration target %s", compatibility.Schema, migrations.LatestVersion)
	}
}

func TestProjectAdmissionPreservesDataOnRejection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.clst")
	db, err := utils.OpenDb(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE config (name TEXT PRIMARY KEY, value TEXT); INSERT INTO config VALUES ('version','2.2'), ('sync_token','unchanged'); CREATE TABLE pending (value TEXT); INSERT INTO pending VALUES ('local work');"); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"data", "sync-token", "chunks", "chunk-upload-confirm", "assets", "collaborators"} {
		t.Run(endpoint, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/project/"+endpoint, nil)
			response := httptest.NewRecorder()
			if admitProjectDatabase(response, request, path) {
				t.Fatal("legacy request admitted")
			}
			if response.Code != http.StatusUpgradeRequired {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
		})
	}
	var token, pending string
	if err := db.Get(&token, "SELECT value FROM config WHERE name='sync_token'"); err != nil {
		t.Fatal(err)
	}
	if err := db.Get(&pending, "SELECT value FROM pending"); err != nil {
		t.Fatal(err)
	}
	if token != "unchanged" || pending != "local work" {
		t.Fatal("rejection changed data")
	}
	request := httptest.NewRequest(http.MethodGet, "/project/data", nil)
	compatibility.Declare(request.Header)
	response := httptest.NewRecorder()
	if !admitProjectDatabase(response, request, path) {
		t.Fatalf("matching contract rejected: %s", response.Body.String())
	}
	if response.Header().Get(compatibility.ProjectSchemaHeader) != compatibility.Schema {
		t.Fatal("missing response contract")
	}
}

func TestNewProjectRoutesAutomaticallyUseAdmission(t *testing.T) {
	oldConfig := CONFIG
	t.Cleanup(func() { CONFIG = oldConfig })
	CONFIG.ProjectsDir = t.TempDir()
	path := filepath.Join(CONFIG.ProjectsDir, "project.clst")
	db, err := utils.OpenDb(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE config (name TEXT PRIMARY KEY, value TEXT); INSERT INTO config VALUES ('version','2.2'); CREATE TABLE user (id TEXT PRIMARY KEY, role_id TEXT); INSERT INTO user VALUES ('alice','role'); CREATE TABLE role (id TEXT PRIMARY KEY); INSERT INTO role VALUES ('role');"); err != nil {
		t.Fatal(err)
	}
	writes := 0
	mux := &projectMux{http.NewServeMux()}
	mux.HandleFunc("POST /{project}/future-writer", func(w http.ResponseWriter, r *http.Request) { writes++ })
	for _, declared := range []bool{false, true} {
		request := httptest.NewRequest(http.MethodPost, "/project/future-writer", nil)
		request = request.WithContext(context.WithValue(request.Context(), apiUserContextKey, []byte(`{"id":"alice"}`)))
		if declared {
			compatibility.Declare(request.Header)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		expected := http.StatusUpgradeRequired
		if declared {
			expected = http.StatusOK
		}
		if response.Code != expected {
			t.Fatalf("declared=%v HTTP %d: %s", declared, response.Code, response.Body.String())
		}
	}
	if writes != 1 {
		t.Fatalf("admitted %d writes", writes)
	}
}
