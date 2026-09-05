package sync_service

import (
	"path/filepath"
	"testing"

	"clustta/internal/repository"
	"clustta/internal/repository/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestCheckpointTagAssignmentRequiresManageDependencies(t *testing.T) {
	db, err := sqlx.Open("sqlite3", filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(repository.ProjectSchema); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO role (id, mtime, name) VALUES ('artist-role', 1, 'artist');
		INSERT INTO user (id, mtime, added_at, first_name, last_name, username, email, role_id)
		VALUES ('artist', 1, 1, 'Studio', 'Artist', 'artist', 'artist@example.com', 'artist-role');
	`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	data := ProjectData{AssetCheckpointTags: []models.AssetCheckpointTag{{Id: "assignment"}}}
	err = AuthorizeProjectDataWrite(tx, "artist", false, data)
	if err == nil {
		t.Fatal("expected checkpoint tag permission error")
	}

	if _, err = tx.Exec("UPDATE role SET manage_dependencies = 1 WHERE id = 'artist-role'"); err != nil {
		t.Fatal(err)
	}
	if err = AuthorizeProjectDataWrite(tx, "artist", false, data); err != nil {
		t.Fatalf("expected manage_dependencies to allow checkpoint tag assignment: %v", err)
	}
}
