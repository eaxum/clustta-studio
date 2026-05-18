// Package studio_integration_service manages per-studio external integration
// configuration (Kitsu, etc.). Service-account credentials are encrypted at
// rest using a master key supplied via the INTEGRATION_SECRET_KEY env var.
package studio_integration_service

import (
	"clustta/internal/cryptoutil"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// ErrNotFound is returned when no config row matches the lookup.
var ErrNotFound = errors.New("studio integration config not found")

// Config is a row in studio_integration_config.
// EncryptedCredentials is never sent over the wire to clients.
type Config struct {
	Id                   string         `db:"id" json:"id"`
	StudioId             string         `db:"studio_id" json:"studio_id"`
	IntegrationId        string         `db:"integration_id" json:"integration_id"`
	ApiUrl               string         `db:"api_url" json:"api_url"`
	EncryptedCredentials string         `db:"encrypted_credentials" json:"-"`
	Enabled              bool           `db:"enabled" json:"enabled"`
	LastValidatedAt      sql.NullInt64  `db:"last_validated_at" json:"-"`
	LastError            string         `db:"last_error" json:"last_error"`
	CreatedAt            int64          `db:"created_at" json:"created_at"`
	UpdatedAt            int64          `db:"updated_at" json:"updated_at"`
}

// Credentials is the plaintext shape we encrypt for Kitsu.
// Other integrations may use different fields; encode whatever is needed.
type Credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Upsert creates or updates the config row for (studio_id, integration_id).
// The provided plaintext credentials are encrypted with masterKey before
// storage; the plaintext is never persisted.
func Upsert(tx *sqlx.Tx, studioId, integrationId, apiUrl string, creds Credentials, masterKey []byte) (Config, error) {
	plaintext, err := json.Marshal(creds)
	if err != nil {
		return Config{}, fmt.Errorf("marshal credentials: %w", err)
	}
	encrypted, err := cryptoutil.Encrypt(masterKey, plaintext)
	if err != nil {
		return Config{}, fmt.Errorf("encrypt credentials: %w", err)
	}

	now := time.Now().Unix()
	existing, err := Get(tx, studioId, integrationId)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return Config{}, err
	}

	if errors.Is(err, ErrNotFound) {
		cfg := Config{
			Id:                   uuid.New().String(),
			StudioId:             studioId,
			IntegrationId:        integrationId,
			ApiUrl:               apiUrl,
			EncryptedCredentials: encrypted,
			Enabled:              true,
			CreatedAt:            now,
			UpdatedAt:            now,
		}
		_, err = tx.NamedExec(`
			INSERT INTO studio_integration_config
				(id, studio_id, integration_id, api_url, encrypted_credentials,
				 enabled, last_error, created_at, updated_at)
			VALUES
				(:id, :studio_id, :integration_id, :api_url, :encrypted_credentials,
				 :enabled, '', :created_at, :updated_at)
		`, cfg)
		if err != nil {
			return Config{}, fmt.Errorf("insert config: %w", err)
		}
		return cfg, nil
	}

	_, err = tx.Exec(`
		UPDATE studio_integration_config
		SET api_url = ?, encrypted_credentials = ?, enabled = 1,
		    last_error = '', updated_at = ?
		WHERE id = ?
	`, apiUrl, encrypted, now, existing.Id)
	if err != nil {
		return Config{}, fmt.Errorf("update config: %w", err)
	}
	existing.ApiUrl = apiUrl
	existing.EncryptedCredentials = encrypted
	existing.Enabled = true
	existing.LastError = ""
	existing.UpdatedAt = now
	return existing, nil
}

// Get retrieves the config for (studio_id, integration_id).
// Returns ErrNotFound if no row exists.
func Get(tx *sqlx.Tx, studioId, integrationId string) (Config, error) {
	var cfg Config
	err := tx.Get(&cfg, `
		SELECT * FROM studio_integration_config
		WHERE studio_id = ? AND integration_id = ?
	`, studioId, integrationId)
	if errors.Is(err, sql.ErrNoRows) {
		return Config{}, ErrNotFound
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// GetDecryptedCredentials returns the plaintext credentials for a config.
// Only the listener should call this; never expose results via HTTP.
func GetDecryptedCredentials(tx *sqlx.Tx, studioId, integrationId string, masterKey []byte) (Credentials, error) {
	cfg, err := Get(tx, studioId, integrationId)
	if err != nil {
		return Credentials{}, err
	}
	return DecryptCredentials(cfg, masterKey)
}

// DecryptCredentials extracts plaintext credentials from an already-loaded Config.
func DecryptCredentials(cfg Config, masterKey []byte) (Credentials, error) {
	plaintext, err := cryptoutil.Decrypt(masterKey, cfg.EncryptedCredentials)
	if err != nil {
		return Credentials{}, fmt.Errorf("decrypt credentials: %w", err)
	}
	var creds Credentials
	if err := json.Unmarshal(plaintext, &creds); err != nil {
		return Credentials{}, fmt.Errorf("unmarshal credentials: %w", err)
	}
	return creds, nil
}

// Delete removes the config row. Returns nil if it did not exist.
func Delete(tx *sqlx.Tx, studioId, integrationId string) error {
	_, err := tx.Exec(`
		DELETE FROM studio_integration_config
		WHERE studio_id = ? AND integration_id = ?
	`, studioId, integrationId)
	return err
}

// List returns all configs across all studios. Used at server startup to
// boot the listener manager.
func List(tx *sqlx.Tx) ([]Config, error) {
	var configs []Config
	err := tx.Select(&configs, `SELECT * FROM studio_integration_config WHERE enabled = 1`)
	if err != nil {
		return nil, err
	}
	return configs, nil
}

// SetLastValidated records a successful credential validation.
func SetLastValidated(tx *sqlx.Tx, id string, ts int64) error {
	_, err := tx.Exec(`
		UPDATE studio_integration_config
		SET last_validated_at = ?, last_error = '', updated_at = ?
		WHERE id = ?
	`, ts, time.Now().Unix(), id)
	return err
}

// SetLastError records a listener or validation failure for the studio admin
// to see in the UI. Does not flip enabled = 0 (operator decides).
func SetLastError(tx *sqlx.Tx, id, errMsg string) error {
	_, err := tx.Exec(`
		UPDATE studio_integration_config
		SET last_error = ?, updated_at = ?
		WHERE id = ?
	`, errMsg, time.Now().Unix(), id)
	return err
}

// SetEnabled flips the enabled flag without altering credentials.
func SetEnabled(tx *sqlx.Tx, id string, enabled bool) error {
	_, err := tx.Exec(`
		UPDATE studio_integration_config
		SET enabled = ?, updated_at = ?
		WHERE id = ?
	`, enabled, time.Now().Unix(), id)
	return err
}
