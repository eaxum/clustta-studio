package repository

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestCreateDependencyTypeReturnsDuplicateNameError(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", ":memory:")
	t.Cleanup(func() { db.Close() })
	db.MustExec(`CREATE TABLE dependency_type (
		id TEXT PRIMARY KEY,
		mtime INTEGER NOT NULL,
		name TEXT UNIQUE NOT NULL,
		synced BOOLEAN DEFAULT 0 NOT NULL
	)`)
	tx := db.MustBegin()
	defer tx.Rollback()

	if _, err := CreateDependencyType(tx, "first-id", "linked"); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateDependencyType(tx, "second-id", "linked"); err == nil {
		t.Fatal("expected duplicate dependency type name to return an error")
	}
}
