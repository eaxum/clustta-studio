package studio_users_service

import (
	"clustta/internal/server/models"
	"clustta/internal/utils"
	"database/sql"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

//go:embed schema.sql
var Schema string

// InitDB initializes the studio users database with schema and default roles
func InitDB(dbPath string) error {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	db, err := sqlx.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create schema from embedded SQL file
	err = utils.CreateSchema(db, Schema)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Initialize default data within a transaction
	tx := db.MustBegin()
	defer tx.Rollback()

	err = initData(tx)
	if err != nil {
		return fmt.Errorf("failed to initialize data: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Run any necessary migrations
	err = runMigrations(db)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Printf("Studio users database initialized: %s", dbPath)
	return nil
}

// initData seeds default roles
func initData(tx *sqlx.Tx) error {
	roles := []string{"admin", "user", "guest"}
	for _, roleName := range roles {
		_, err := GetOrCreateRole(tx, roleName)
		if err != nil {
			return fmt.Errorf("failed to create role '%s': %w", roleName, err)
		}
	}
	return nil
}

// GetOrCreateRole returns an existing role by name, or creates it with a new UUID
func GetOrCreateRole(tx *sqlx.Tx, name string) (models.Role, error) {
	var role models.Role
	query := "SELECT * FROM role WHERE name = ?"
	err := tx.Get(&role, query, name)
	if err != nil {
		if err == sql.ErrNoRows {
			// Create new role with UUID
			role.Id = uuid.New().String()
			role.Name = name
			role.MTime = 0

			_, err = tx.Exec("INSERT INTO role (id, mtime, name) VALUES (?, ?, ?)",
				role.Id, role.MTime, role.Name)
			if err != nil {
				return role, fmt.Errorf("failed to insert role: %w", err)
			}

			// Fetch the newly created role
			err = tx.Get(&role, query, name)
			if err != nil {
				return role, fmt.Errorf("failed to fetch created role: %w", err)
			}
		} else {
			return role, err
		}
	}
	return role, nil
}

// GetRoleByName fetches a role by its name
func GetRoleByName(tx *sqlx.Tx, name string) (models.Role, error) {
	var role models.Role
	query := "SELECT * FROM role WHERE name = ?"
	err := tx.Get(&role, query, name)
	if err != nil {
		return role, err
	}
	return role, nil
}

// GetRoles returns all roles
func GetRoles(tx *sqlx.Tx) ([]models.Role, error) {
	var roles []models.Role
	err := tx.Select(&roles, "SELECT * FROM role")
	if err != nil {
		return roles, err
	}
	return roles, nil
}

// runMigrations handles schema migrations for studio users DB
func runMigrations(db *sqlx.DB) error {
	// Check if role_id column exists in user table
	exists, err := utils.IsColumnExist(db, "user", "role_id")
	if err != nil {
		return err
	}

	if !exists {
		// Add the column
		err = utils.AddColumnIfNotExist(db, "user", "role_id", "TEXT", "", true)
		if err != nil {
			return fmt.Errorf("failed to add role_id column: %w", err)
		}

		// Backfill existing users with roles
		tx := db.MustBegin()
		defer tx.Rollback()

		adminRole, err := GetRoleByName(tx, "admin")
		if err != nil {
			return fmt.Errorf("failed to get admin role: %w", err)
		}

		userRole, err := GetRoleByName(tx, "user")
		if err != nil {
			return fmt.Errorf("failed to get user role: %w", err)
		}

		// Find the earliest registered user and assign admin
		var firstUserId string
		err = tx.Get(&firstUserId, "SELECT id FROM user ORDER BY added_at ASC LIMIT 1")
		if err != nil && err != sql.ErrNoRows {
			return fmt.Errorf("failed to find first user: %w", err)
		}

		if firstUserId != "" {
			// Assign admin to first user
			_, err = tx.Exec("UPDATE user SET role_id = ? WHERE id = ?", adminRole.Id, firstUserId)
			if err != nil {
				return fmt.Errorf("failed to assign admin role: %w", err)
			}

			// Assign 'user' role to everyone else
			_, err = tx.Exec("UPDATE user SET role_id = ? WHERE role_id IS NULL", userRole.Id)
			if err != nil {
				return fmt.Errorf("failed to assign user role: %w", err)
			}

			log.Println("Migrated existing users with role assignments")
		}

		if err = tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit migration: %w", err)
		}
	}

	return nil
}
