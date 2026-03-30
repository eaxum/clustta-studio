package api_token_service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

const tokenLifetime = 30 * 24 * time.Hour // 30 days

// CreateToken generates a new API token for the given user.
// Returns the raw token to send to the client (never stored).
func CreateToken(db *sqlx.DB, userId string) (string, error) {
	rawBytes := make([]byte, 32)
	_, err := rand.Read(rawBytes)
	if err != nil {
		return "", fmt.Errorf("failed to generate token: %w", err)
	}
	rawToken := hex.EncodeToString(rawBytes)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	now := time.Now().Unix()
	expiresAt := time.Now().Add(tokenLifetime).Unix()

	_, err = db.Exec(
		"INSERT INTO api_token (id, token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		uuid.New().String(), tokenHash, userId, now, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("failed to store token: %w", err)
	}

	return rawToken, nil
}

// ValidateToken looks up a raw token and returns the user ID if valid.
func ValidateToken(db *sqlx.DB, rawToken string) (string, error) {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var userId string
	var expiresAt int64
	err := db.QueryRow(
		"SELECT user_id, expires_at FROM api_token WHERE token_hash = ?", tokenHash,
	).Scan(&userId, &expiresAt)
	if err != nil {
		return "", fmt.Errorf("invalid token")
	}

	if time.Now().Unix() > expiresAt {
		db.Exec("DELETE FROM api_token WHERE token_hash = ?", tokenHash)
		return "", fmt.Errorf("token expired")
	}

	return userId, nil
}

// DeleteToken removes a specific token (for logout).
func DeleteToken(db *sqlx.DB, rawToken string) error {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	_, err := db.Exec("DELETE FROM api_token WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteUserTokens removes all tokens for a user.
func DeleteUserTokens(db *sqlx.DB, userId string) error {
	_, err := db.Exec("DELETE FROM api_token WHERE user_id = ?", userId)
	return err
}

// CleanupExpired removes all expired tokens.
func CleanupExpired(db *sqlx.DB) error {
	_, err := db.Exec("DELETE FROM api_token WHERE expires_at < ?", time.Now().Unix())
	return err
}
