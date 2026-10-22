package api_token_service

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

func TestValidateTokenRejectsInactiveAndDeletedUsers(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", ":memory:")
	defer db.Close()
	db.MustExec(`CREATE TABLE user (id TEXT PRIMARY KEY, active INTEGER, is_deleted INTEGER)`)
	db.MustExec(`CREATE TABLE api_token (id TEXT, token_hash TEXT, user_id TEXT, created_at INTEGER, expires_at INTEGER)`)
	db.MustExec(`INSERT INTO user (id, active, is_deleted) VALUES ('user-1', 1, 0)`)

	token, err := CreateToken(db, "user-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateToken(db, token); err != nil {
		t.Fatalf("active user token rejected: %v", err)
	}

	db.MustExec(`UPDATE user SET active = 0 WHERE id = 'user-1'`)
	if _, err := ValidateToken(db, token); err == nil {
		t.Fatal("inactive user token was accepted")
	}

	db.MustExec(`UPDATE user SET active = 1, is_deleted = 1 WHERE id = 'user-1'`)
	if _, err := ValidateToken(db, token); err == nil {
		t.Fatal("deleted user token was accepted")
	}
}
