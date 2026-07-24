package chunk_service

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"clustta/internal/utils"
	"github.com/jmoiron/sqlx"
)

const conversionSchema = `
CREATE TABLE IF NOT EXISTS project_storage_conversion (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    source_mode TEXT NOT NULL CHECK (source_mode IN ('compact', 'deflated')),
    target_mode TEXT NOT NULL CHECK (target_mode IN ('compact', 'deflated')),
    status TEXT NOT NULL CHECK (status IN ('running', 'failed', 'cleanup_failed', 'completed')),
    total_chunks INTEGER NOT NULL DEFAULT 0,
    processed_chunks INTEGER NOT NULL DEFAULT 0,
    required_bytes INTEGER NOT NULL DEFAULT 0,
    processed_bytes INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
)`

type StorageConversionState struct {
	CurrentMode     string `db:"current_mode" json:"current_mode"`
	SourceMode      string `db:"source_mode" json:"source_mode"`
	TargetMode      string `db:"target_mode" json:"target_mode"`
	Status          string `db:"status" json:"status"`
	TotalChunks     int64  `db:"total_chunks" json:"total_chunks"`
	ProcessedChunks int64  `db:"processed_chunks" json:"processed_chunks"`
	RequiredBytes   int64  `db:"required_bytes" json:"required_bytes"`
	ProcessedBytes  int64  `db:"processed_bytes" json:"processed_bytes"`
	Error           string `db:"error" json:"error"`
	StartedAt       int64  `db:"started_at" json:"started_at"`
	UpdatedAt       int64  `db:"updated_at" json:"updated_at"`
}

var (
	projectLocks      sync.Map
	activeConversions sync.Map
)

func projectLock(projectPath string) *sync.RWMutex {
	key := filepath.Clean(projectPath)
	value, _ := projectLocks.LoadOrStore(key, &sync.RWMutex{})
	return value.(*sync.RWMutex)
}

func LockProjectRequest(projectPath string) func() {
	lock := projectLock(projectPath)
	lock.RLock()
	return lock.RUnlock
}

func ProjectConversionActive(projectPath string) bool {
	_, active := activeConversions.Load(filepath.Clean(projectPath))
	return active
}

func ensureConversionSchema(db *sqlx.DB) error {
	_, err := db.Exec(conversionSchema)
	return err
}

func GetStorageConversionState(projectPath string) (StorageConversionState, error) {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return StorageConversionState{}, err
	}
	defer db.Close()
	if err := ensureConversionSchema(db); err != nil {
		return StorageConversionState{}, err
	}
	state, err := getStorageConversionState(db)
	if err != nil {
		return state, err
	}
	if state.Status == "idle" || state.Status == "completed" {
		table := "chunk"
		targetMode := StorageModeDeflated
		if state.CurrentMode == StorageModeDeflated {
			table = "chunk_ref"
			targetMode = StorageModeCompact
		}
		var payloadBytes int64
		_ = db.QueryRowx("SELECT COUNT(*), COALESCE(SUM(size), 0) FROM "+table).Scan(&state.TotalChunks, &payloadBytes)
		state.RequiredBytes = requiredDestinationBytes(payloadBytes, targetMode)
	}
	return state, nil
}

func getStorageConversionState(db *sqlx.DB) (StorageConversionState, error) {
	var state StorageConversionState
	err := db.Get(&state, `
		SELECT ps.mode AS current_mode, c.source_mode, c.target_mode, c.status,
		       c.total_chunks, c.processed_chunks, c.required_bytes, c.processed_bytes,
		       c.error, c.started_at, c.updated_at
		FROM project_storage ps JOIN project_storage_conversion c ON c.id = 1
		WHERE ps.id = 1`)
	if errors.Is(err, sql.ErrNoRows) {
		if err := db.Get(&state.CurrentMode, "SELECT mode FROM project_storage WHERE id = 1"); err != nil {
			return state, err
		}
		state.Status = "idle"
		return state, nil
	}
	return state, err
}

// RecoverInterruptedStorageConversion makes a Studio restart explicitly
// retryable. The authoritative source mode is never changed mid-copy.
func RecoverInterruptedStorageConversion(projectPath string) error {
	db, err := utils.OpenDb(projectPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := ensureConversionSchema(db); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE project_storage_conversion
		SET status = 'failed', error = 'Conversion interrupted by Studio restart; retry to continue.', updated_at = unixepoch()
		WHERE id = 1 AND status = 'running'`)
	if err != nil {
		return err
	}
	state, err := getStorageConversionState(db)
	if err != nil {
		return err
	}
	// A crash can occur after the atomic Deflated-to-Compact switch but before
	// the now-non-authoritative external directory is removed.
	if state.CurrentMode == StorageModeCompact && state.SourceMode == StorageModeDeflated && state.TargetMode == StorageModeCompact && (state.Status == "completed" || state.Status == "cleanup_failed") {
		if err := cleanupDeflatedSource(db); err != nil {
			_, _ = db.Exec(`UPDATE project_storage_conversion SET status='cleanup_failed', error=?, updated_at=unixepoch() WHERE id=1`, err.Error())
			return err
		}
	}
	return nil
}

func StartStorageConversion(projectPath, targetMode string, availableBytes int64) (StorageConversionState, error) {
	if targetMode != StorageModeCompact && targetMode != StorageModeDeflated {
		return StorageConversionState{}, fmt.Errorf("unsupported conversion target %q", targetMode)
	}
	if err := ValidateStorageMode(targetMode); err != nil {
		return StorageConversionState{}, err
	}
	key := filepath.Clean(projectPath)
	if _, loaded := activeConversions.LoadOrStore(key, struct{}{}); loaded {
		return StorageConversionState{}, errors.New("project storage conversion is already running")
	}
	lock := projectLock(key)
	lock.Lock()

	db, err := utils.OpenDb(key)
	if err != nil {
		lock.Unlock()
		activeConversions.Delete(key)
		return StorageConversionState{}, err
	}
	if err = ensureConversionSchema(db); err != nil {
		db.Close()
		lock.Unlock()
		activeConversions.Delete(key)
		return StorageConversionState{}, err
	}
	state, err := prepareStorageConversion(db, targetMode, availableBytes)
	if err != nil {
		db.Close()
		lock.Unlock()
		activeConversions.Delete(key)
		return StorageConversionState{}, err
	}

	go func() {
		defer db.Close()
		defer lock.Unlock()
		defer activeConversions.Delete(key)
		if err := runStorageConversion(db, state); err != nil {
			status := "failed"
			current, stateErr := getStorageConversionState(db)
			if stateErr == nil && current.CurrentMode == StorageModeCompact && state.SourceMode == StorageModeDeflated && state.TargetMode == StorageModeCompact {
				status = "cleanup_failed"
			}
			_, _ = db.Exec(`UPDATE project_storage_conversion SET status = ?, error = ?, updated_at = unixepoch() WHERE id = 1`, status, err.Error())
		}
	}()
	return state, nil
}

func prepareStorageConversion(db *sqlx.DB, targetMode string, availableBytes int64) (StorageConversionState, error) {
	state, err := getStorageConversionState(db)
	if err != nil {
		return state, err
	}
	if state.Status == "cleanup_failed" && state.CurrentMode == StorageModeCompact && targetMode == StorageModeCompact {
		state.Status = "running"
		_, err = db.Exec(`UPDATE project_storage_conversion SET status = 'running', error = '', updated_at = unixepoch() WHERE id = 1`)
		return state, err
	}
	if state.CurrentMode == targetMode {
		return state, fmt.Errorf("project already uses %s storage", targetMode)
	}
	if state.CurrentMode != StorageModeCompact && state.CurrentMode != StorageModeDeflated {
		return state, fmt.Errorf("storage mode %q cannot be converted", state.CurrentMode)
	}

	var total, required int64
	table := "chunk"
	if state.CurrentMode == StorageModeDeflated {
		table = "chunk_ref"
	}
	if err := db.QueryRowx("SELECT COUNT(*), COALESCE(SUM(size), 0) FROM "+table).Scan(&total, &required); err != nil {
		return state, err
	}
	required = requiredDestinationBytes(required, targetMode)
	if availableBytes >= 0 && required > availableBytes {
		return state, fmt.Errorf("conversion requires %d bytes but only %d bytes are available", required, availableBytes)
	}
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO project_storage_conversion
		(id, source_mode, target_mode, status, total_chunks, processed_chunks, required_bytes, processed_bytes, error, started_at, updated_at)
		VALUES (1, ?, ?, 'running', ?, 0, ?, 0, '', ?, ?)
		ON CONFLICT(id) DO UPDATE SET source_mode=excluded.source_mode, target_mode=excluded.target_mode,
		status='running', total_chunks=excluded.total_chunks, processed_chunks=0,
		required_bytes=excluded.required_bytes, processed_bytes=0, error='',
		started_at=excluded.started_at, updated_at=excluded.updated_at`, state.CurrentMode, targetMode, total, required, now, now)
	if err != nil {
		return state, err
	}
	state.SourceMode = state.CurrentMode
	state.TargetMode = targetMode
	state.Status = "running"
	state.TotalChunks = total
	state.RequiredBytes = required
	state.ProcessedChunks = 0
	state.ProcessedBytes = 0
	state.Error = ""
	state.StartedAt = now
	state.UpdatedAt = now
	return state, nil
}

func requiredDestinationBytes(payloadBytes int64, targetMode string) int64 {
	if payloadBytes == 0 {
		return 0
	}
	// Compact needs additional room for SQLite's WAL and checkpoint while the
	// destination payload is materialized. Both modes include allocation slack.
	if targetMode == StorageModeCompact {
		return payloadBytes*22/10 + 16*1024*1024
	}
	return payloadBytes*11/10 + 16*1024*1024
}

type conversionChunk struct {
	Hash string `db:"hash"`
	Data []byte `db:"data"`
	Size int    `db:"size"`
}

func runStorageConversion(db *sqlx.DB, state StorageConversionState) error {
	if state.Status == "running" && state.CurrentMode == StorageModeCompact && state.SourceMode == StorageModeDeflated && state.TargetMode == StorageModeCompact {
		return cleanupDeflatedSource(db)
	}
	if state.SourceMode == StorageModeCompact {
		if err := copyCompactToDeflated(db); err != nil {
			return err
		}
		if err := verifyCompactToDeflated(db); err != nil {
			return err
		}
		if err := finalizeConversion(db, StorageModeDeflated); err != nil {
			return err
		}
		// The atomic switch deletes Compact rows; VACUUM then releases their
		// pages while the project remains unavailable under the conversion lock.
		if _, err := db.Exec(`VACUUM`); err != nil {
			_, _ = db.Exec(`UPDATE project_storage_conversion SET error = ?, updated_at = unixepoch() WHERE id = 1`, "Conversion completed, but the archive could not be compacted: "+err.Error())
		}
		return nil
	}
	if err := copyDeflatedToCompact(db); err != nil {
		return err
	}
	if err := verifyDeflatedToCompact(db); err != nil {
		return err
	}
	if err := finalizeConversion(db, StorageModeCompact); err != nil {
		return err
	}
	return cleanupDeflatedSource(db)
}

func copyCompactToDeflated(db *sqlx.DB) error {
	var lastHash string
	for {
		var chunks []conversionChunk
		if err := db.Select(&chunks, `SELECT hash, data, size FROM chunk WHERE hash > ? ORDER BY hash LIMIT 100`, lastHash); err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		for _, chunk := range chunks {
			tx, err := db.Beginx()
			if err != nil {
				return err
			}
			path, _, err := deflatedChunkPath(tx, chunk.Hash)
			if err == nil {
				existing, readErr := os.ReadFile(path)
				if readErr == nil && !bytes.Equal(existing, chunk.Data) {
					err = os.Remove(path)
				} else if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
					err = readErr
				}
			}
			if err == nil {
				err = storeDeflatedChunk(tx, chunk.Hash, chunk.Data, chunk.Size)
			}
			if err == nil {
				_, err = tx.Exec(`UPDATE project_storage_conversion SET processed_chunks=processed_chunks+1, processed_bytes=processed_bytes+?, updated_at=unixepoch() WHERE id=1`, chunk.Size)
			}
			if err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			lastHash = chunk.Hash
		}
	}
}

func copyDeflatedToCompact(db *sqlx.DB) error {
	var lastHash string
	for {
		var refs []conversionChunk
		if err := db.Select(&refs, `SELECT hash, NULL AS data, size FROM chunk_ref WHERE hash > ? ORDER BY hash LIMIT 100`, lastHash); err != nil {
			return err
		}
		if len(refs) == 0 {
			return nil
		}
		for _, ref := range refs {
			tx, err := db.Beginx()
			if err != nil {
				return err
			}
			path, _, err := deflatedChunkPath(tx, ref.Hash)
			var data []byte
			if err == nil {
				data, err = os.ReadFile(path)
			}
			if err == nil {
				_, err = tx.Exec(`INSERT INTO chunk (hash, data, size) VALUES (?, ?, ?)
					ON CONFLICT(hash) DO UPDATE SET data=excluded.data, size=excluded.size`, ref.Hash, data, ref.Size)
			}
			if err == nil {
				_, err = tx.Exec(`UPDATE project_storage_conversion SET processed_chunks=processed_chunks+1, processed_bytes=processed_bytes+?, updated_at=unixepoch() WHERE id=1`, ref.Size)
			}
			if err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
			lastHash = ref.Hash
		}
	}
}

func verifyCompactToDeflated(db *sqlx.DB) error {
	var lastHash string
	for {
		var chunks []conversionChunk
		if err := db.Select(&chunks, `SELECT hash, data, size FROM chunk WHERE hash > ? ORDER BY hash LIMIT 100`, lastHash); err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		for _, chunk := range chunks {
			tx, err := db.Beginx()
			if err != nil {
				return err
			}
			path, _, err := deflatedChunkPath(tx, chunk.Hash)
			tx.Rollback()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, chunk.Data) {
				return fmt.Errorf("verification failed for chunk %s", chunk.Hash)
			}
			lastHash = chunk.Hash
		}
	}
}

func verifyDeflatedToCompact(db *sqlx.DB) error {
	var lastHash string
	for {
		var refs []conversionChunk
		if err := db.Select(&refs, `SELECT hash, NULL AS data, size FROM chunk_ref WHERE hash > ? ORDER BY hash LIMIT 100`, lastHash); err != nil {
			return err
		}
		if len(refs) == 0 {
			return nil
		}
		for _, ref := range refs {
			var compact []byte
			if err := db.Get(&compact, `SELECT data FROM chunk WHERE hash = ?`, ref.Hash); err != nil {
				return fmt.Errorf("verification failed for chunk %s: %w", ref.Hash, err)
			}
			tx, err := db.Beginx()
			if err != nil {
				return err
			}
			path, _, err := deflatedChunkPath(tx, ref.Hash)
			tx.Rollback()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, compact) {
				return fmt.Errorf("verification failed for chunk %s", ref.Hash)
			}
			lastHash = ref.Hash
		}
	}
}

func finalizeConversion(db *sqlx.DB, targetMode string) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`UPDATE project_storage SET mode = ?, updated_at = unixepoch() WHERE id = 1`, targetMode); err != nil {
		return err
	}
	if targetMode == StorageModeDeflated {
		_, err = tx.Exec(`DELETE FROM chunk`)
	} else {
		_, err = tx.Exec(`DELETE FROM chunk_ref`)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE project_storage_conversion SET status='completed', processed_chunks=total_chunks, processed_bytes=required_bytes, error='', updated_at=unixepoch() WHERE id=1`); err != nil {
		return err
	}
	return tx.Commit()
}

func cleanupDeflatedSource(db *sqlx.DB) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	path, err := deflatedProjectPath(tx)
	tx.Rollback()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE project_storage_conversion SET status='completed', error='', updated_at=unixepoch() WHERE id=1`)
	return err
}
