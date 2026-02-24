package session_service

import (
	"clustta/internal/utils"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"
)

//go:embed schema.sql
var Schema string

// InitDB initializes the session database schema (closes connection after)
func InitDB(sessionDB string, walMode bool) error {
	db, err := sqlx.Open("sqlite3", sessionDB)
	if err != nil {
		return err
	}
	defer db.Close()

	if walMode {
		_, err = db.Exec("PRAGMA journal_mode = WAL;")
		if err != nil {
			return err
		}
	}

	err = utils.CreateSchema(db, Schema)
	if err != nil {
		return fmt.Errorf("error creating sessions table: %v", err)
	}
	return nil
}

// OpenDB initializes the session database and returns an open connection.
// The caller is responsible for closing the returned *sql.DB.
func OpenDB(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create schema using sqlx wrapper for CreateSchema compatibility
	sqlxDb := sqlx.NewDb(db, "sqlite3")
	err = utils.CreateSchema(sqlxDb, Schema)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	log.Printf("Session database initialized: %s", dbPath)
	return db, nil
}

func UpdateDB(sessionDB string) error {
	db, err := sqlx.Open("sqlite3", sessionDB)
	if err != nil {
		return err
	}
	defer db.Close()

	err = utils.CreateSchema(db, Schema)
	if err != nil {
		return fmt.Errorf("error creating sessions table: %v", err)
	}
	return nil
}
