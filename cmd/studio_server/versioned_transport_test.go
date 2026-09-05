package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clustta/internal/constants"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"

	"github.com/DataDog/zstd"
	"github.com/jmoiron/sqlx"
	"google.golang.org/protobuf/proto"
)

type photoTestTransport struct{}

func (photoTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(r.URL.Path, "/photo") {
		return nil, fmt.Errorf("unexpected external request: %s", r.URL)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
}

// TestClientWireRoundTrip runs client-generated payloads through production data handlers.
func TestClientWireRoundTrip(t *testing.T) {
	directory := os.Getenv("CLUSTTA_WIRE_FIXTURES")
	if directory == "" {
		t.Skip("set CLUSTTA_WIRE_FIXTURES after generating client fixtures")
	}
	oldDirectory, oldTransport, oldPrivate := CONFIG.ProjectsDir, http.DefaultTransport, constants.PrivateMode
	CONFIG.ProjectsDir = t.TempDir()
	http.DefaultTransport = photoTestTransport{}
	constants.PrivateMode = false
	defer func() {
		CONFIG.ProjectsDir, http.DefaultTransport, constants.PrivateMode = oldDirectory, oldTransport, oldPrivate
	}()
	db := sqlx.MustOpen("sqlite3", filepath.Join(CONFIG.ProjectsDir, "wire.clst"))
	defer db.Close()
	db.MustExec(repository.ProjectSchema)
	db.MustExec("INSERT INTO config (name, value, mtime) VALUES ('working_dir', ?, 1)", CONFIG.ProjectsDir)
	db.MustExec(`
		INSERT INTO config (name, value, mtime) VALUES ('version', '2.2', 1);
		INSERT INTO role (id, mtime, name, view_asset, view_collection, manage_dependencies, update_asset)
		VALUES ('artist-role', 1, 'artist', 1, 1, 1, 1);
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Test', 'Artist', 'artist', 'artist@example.com', 'artist-role');
		INSERT INTO status (id, mtime, name, short_name) VALUES ('status', 1, 'Ready', 'ready');
		INSERT INTO asset_type (id, mtime, name, icon) VALUES ('type', 1, 'Generic', 'generic');
		INSERT INTO dependency_type (id, mtime, name) VALUES ('default', 1, 'default');
		INSERT INTO tag (id, mtime, name) VALUES ('approved-tag', 1, 'approved');
		INSERT INTO asset (id, created_at, mtime, name, extension, status_id, asset_type_id)
		VALUES ('boy', 1, 1, 'Boy', '.blend', 'status', 'type'), ('shot', 1, 1, 'Shot', '.blend', 'status', 'type');
		INSERT INTO asset_checkpoint (id, created_at, mtime, asset_id, xxhash_checksum, time_modified, file_size, chunks, author_id)
		VALUES ('boy-v1', 1, 1, 'boy', 'hash1', 1, 0, '', 'artist'), ('boy-v2', 2, 2, 'boy', 'hash2', 2, 0, '', 'artist');
	`)
	for step := 0; step < 5; step++ {
		path := filepath.Join(directory, fmt.Sprintf("%02d", step))
		body, err := os.ReadFile(path + ".request")
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/project/wire/data", bytes.NewReader(body))
		request.SetPathValue("project", "wire")
		request = request.WithContext(context.WithValue(request.Context(), apiUserContextKey, []byte(`{"id":"artist"}`)))
		response := httptest.NewRecorder()
		PostDataHandler(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("step %d push: %d %s", step, response.Code, response.Body.String())
		}
		request = httptest.NewRequest(http.MethodGet, "/project/wire/data", strings.NewReader(`{"user_id":"artist"}`))
		request.SetPathValue("project", "wire")
		request = request.WithContext(context.WithValue(request.Context(), apiUserContextKey, []byte(`{"id":"artist"}`)))
		response = httptest.NewRecorder()
		GetDataHandler(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("step %d pull: %d %s", step, response.Code, response.Body.String())
		}
		decoded, err := zstd.Decompress(nil, response.Body.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		var data repositorypb.ProjectData
		if err := proto.Unmarshal(decoded, &data); err != nil || len(data.AssetDependencies) != 1 {
			t.Fatalf("invalid round-trip: %v %+v", err, &data)
		}
		if err := os.WriteFile(path+".response", response.Body.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
