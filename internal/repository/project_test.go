package repository

import (
	"os"
	"path/filepath"
	"testing"

	"clustta/internal/error_service"
)

func TestUpdateProjectRejectsMissingConfig(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "invalid.clst")
	if err := os.WriteFile(projectPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProject(projectPath); err != error_service.ErrInvalidProject {
		t.Fatalf("expected invalid project error, got %v", err)
	}
}
