package repository

import (
	"clustta/internal/compatibility"
	"clustta/internal/repository/migrations"
	"testing"
)

func TestProjectSchemaMatchesMigrationTarget(t *testing.T) {
	if compatibility.CurrentProjectSchema != migrations.LatestVersion {
		t.Fatalf("project schema %s does not match migration target %s", compatibility.CurrentProjectSchema, migrations.LatestVersion)
	}
}
