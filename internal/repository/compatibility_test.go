package repository

import (
	"clustta/internal/auth_service"
	"clustta/internal/compatibility"
	"clustta/internal/repository/migrations"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestCompatibilityMatchesMigrationTarget(t *testing.T) {
	if compatibility.Schema != migrations.LatestVersion {
		t.Fatalf("contract schema %s does not match migration target %s", compatibility.Schema, migrations.LatestVersion)
	}
}

func TestDiscoveryReadsIncompatibleProjectFromStableConfig(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "future.clst")
	db := sqlx.MustOpen("sqlite3", projectPath)
	db.MustExec(`CREATE TABLE config (name TEXT PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO config VALUES ('version', '2.3'), ('project_id', 'future-id'), ('project_name', 'Future');`)
	db.Close()

	project, err := GetProjectDiscoveryInfo(projectPath, auth_service.User{})
	if err != nil {
		t.Fatal(err)
	}
	if project.Version != "2.3" || project.Compatibility == nil || project.Compatibility.ProjectSchema != "2.3" {
		t.Fatalf("unexpected discovery result: %+v", project)
	}
}
