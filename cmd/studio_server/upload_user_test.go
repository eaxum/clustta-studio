package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"clustta/internal/compatibility"
	"clustta/internal/constants"
	"clustta/internal/repository"
	"clustta/internal/repository/repositorypb"

	"github.com/DataDog/zstd"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type rejectPhotoRequestTransport struct{}

func (rejectPhotoRequestTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected photo request")
}

func TestPostDataHandlerAcceptsImportedUserOutsideStudioDirectory(t *testing.T) {
	oldDirectory, oldTransport, oldPrivate := CONFIG.ProjectsDir, http.DefaultTransport, constants.PrivateMode
	CONFIG.ProjectsDir = t.TempDir()
	http.DefaultTransport = rejectPhotoRequestTransport{}
	constants.PrivateMode = false
	t.Cleanup(func() {
		CONFIG.ProjectsDir = oldDirectory
		http.DefaultTransport = oldTransport
		constants.PrivateMode = oldPrivate
	})

	projectPath := filepath.Join(CONFIG.ProjectsDir, "upload.clst")
	db := sqlx.MustOpen("sqlite3", projectPath)
	t.Cleanup(func() { db.Close() })
	db.MustExec(repository.ProjectSchema)
	db.MustExec(`
		INSERT INTO config (name, value, mtime) VALUES ('working_dir', '', 1), ('version', '2.2', 1);
		INSERT INTO role (id, mtime, name, add_user) VALUES ('admin-role', 1, 'admin', 1);
		INSERT INTO user (id, mtime, added_at, username, email, first_name, last_name, role_id)
		VALUES ('admin', 1, '2026-01-01', 'admin', 'admin@example.com', 'Admin', 'User', 'admin-role');
	`)

	payload, err := proto.Marshal(&repositorypb.ProjectData{
		Users: []*repositorypb.User{{
			Id: "archive-user", Mtime: 1, AddedAt: "2025-01-01", Username: "archive",
			Email: "archive@example.com", FirstName: "Archive", LastName: "User", RoleId: "admin-role",
		}},
	})
	require.NoError(t, err)
	compressed, err := zstd.Compress(nil, payload)
	require.NoError(t, err)

	request := httptest.NewRequest(http.MethodPost, "/upload/data", bytes.NewReader(compressed))
	request.SetPathValue("project", "upload")
	request = request.WithContext(context.WithValue(request.Context(), apiUserContextKey, []byte(`{"id":"admin"}`)))
	request = request.WithContext(compatibility.WithAPIContext(request.Context(), compatibility.APIContext{Version: compatibility.CurrentAPIVersion}))
	response := httptest.NewRecorder()

	PostDataHandler(response, request)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var count int
	require.NoError(t, db.Get(&count, "SELECT count(*) FROM user WHERE id = 'archive-user'"))
	require.Equal(t, 1, count)
}
