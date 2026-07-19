package chunk_service

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
)

const (
	StorageModeCompact       = "compact"
	StorageModeDeflated      = "deflated"
	StorageModeObjectStorage = "object_storage"
)

var (
	storageConfigMu sync.RWMutex
	storageRoot     string
)

// ConfigureProjectStorage validates and stores the Studio-wide storage root.
// An empty root intentionally leaves Deflated storage unavailable.
func ConfigureProjectStorage(root string) error {
	storageConfigMu.Lock()
	defer storageConfigMu.Unlock()

	storageRoot = ""
	if strings.TrimSpace(root) == "" {
		return nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve storage directory: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0750); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}
	probe, err := os.CreateTemp(absRoot, ".clustta-write-check-*")
	if err != nil {
		return fmt.Errorf("storage directory is not writable: %w", err)
	}
	probeName := probe.Name()
	if err := probe.Close(); err != nil {
		os.Remove(probeName)
		return fmt.Errorf("close storage directory write check: %w", err)
	}
	if err := os.Remove(probeName); err != nil {
		return fmt.Errorf("clean storage directory write check: %w", err)
	}
	storageRoot = absRoot
	return nil
}

func StorageDirectoryAvailable() bool {
	storageConfigMu.RLock()
	defer storageConfigMu.RUnlock()
	return storageRoot != ""
}

func SupportedStorageModes() []string {
	return []string{StorageModeCompact, StorageModeDeflated}
}

func AvailableStorageModes() []string {
	modes := []string{StorageModeCompact}
	if StorageDirectoryAvailable() {
		modes = append(modes, StorageModeDeflated)
	}
	return modes
}

func ValidateStorageMode(mode string) error {
	switch mode {
	case StorageModeCompact:
		return nil
	case StorageModeDeflated:
		if !StorageDirectoryAvailable() {
			return errors.New("deflated storage is not configured")
		}
		return nil
	case StorageModeObjectStorage:
		return errors.New("object storage is not implemented")
	default:
		return fmt.Errorf("unsupported storage mode %q", mode)
	}
}

// GetProjectStorageMode returns Compact for legacy databases without the
// server-owned storage metadata table or row.
func GetProjectStorageMode(tx *sqlx.Tx) (string, error) {
	var mode string
	err := tx.Get(&mode, "SELECT mode FROM project_storage WHERE id = 1")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "no such table") {
			return StorageModeCompact, nil
		}
		return "", err
	}
	switch mode {
	case StorageModeCompact, StorageModeDeflated, StorageModeObjectStorage:
		return mode, nil
	default:
		return "", fmt.Errorf("project has invalid storage mode %q", mode)
	}
}

func SetProjectStorageMode(tx *sqlx.Tx, mode string) error {
	if err := ValidateStorageMode(mode); err != nil {
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO project_storage (id, mode, updated_at)
		VALUES (1, ?, unixepoch())
		ON CONFLICT(id) DO UPDATE SET mode = excluded.mode, updated_at = excluded.updated_at
	`, mode)
	return err
}

func projectID(tx *sqlx.Tx) (string, error) {
	var id string
	if err := tx.Get(&id, "SELECT value FROM config WHERE name = 'project_id'"); err != nil {
		return "", err
	}
	if id == "" || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return "", errors.New("invalid project id for storage")
	}
	return id, nil
}

func validateChunkHash(hash string) error {
	if len(hash) != 64 {
		return errors.New("invalid chunk hash length")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return errors.New("invalid chunk hash")
	}
	return nil
}

func deflatedChunkPath(tx *sqlx.Tx, hash string) (string, string, error) {
	if err := validateChunkHash(hash); err != nil {
		return "", "", err
	}
	storageConfigMu.RLock()
	root := storageRoot
	storageConfigMu.RUnlock()
	if root == "" {
		return "", "", errors.New("deflated storage is not configured")
	}
	id, err := projectID(tx)
	if err != nil {
		return "", "", err
	}
	key := filepath.Join(id, "chunks", hash[:2], hash[2:4], hash)
	return filepath.Join(root, key), filepath.ToSlash(key), nil
}

// StoreChunk stores compressed chunk bytes using the project's selected mode.
func StoreChunk(tx *sqlx.Tx, hash string, data []byte, size int) error {
	mode, err := GetProjectStorageMode(tx)
	if err != nil {
		return err
	}
	if mode == StorageModeCompact {
		_, err = tx.Exec("INSERT OR IGNORE INTO chunk (hash, data, size) VALUES (?, ?, ?)", hash, data, size)
		return err
	}
	if mode != StorageModeDeflated {
		return fmt.Errorf("storage mode %q is not available", mode)
	}

	path, key, err := deflatedChunkPath(tx, hash)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		tmp, err := os.CreateTemp(filepath.Dir(path), ".chunk-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		removeTemp := true
		defer func() {
			if removeTemp {
				os.Remove(tmpName)
			}
		}()
		if _, err = tmp.Write(data); err != nil {
			tmp.Close()
			return err
		}
		if err = tmp.Sync(); err != nil {
			tmp.Close()
			return err
		}
		if err = tmp.Close(); err != nil {
			return err
		}
		if err = os.Rename(tmpName, path); err != nil {
			if _, statErr := os.Stat(path); statErr != nil {
				return err
			}
		} else {
			removeTemp = false
		}
	} else if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT OR IGNORE INTO chunk_ref (hash, storage_key, size, created_at)
		VALUES (?, ?, ?, unixepoch())
	`, hash, key, size)
	return err
}

func ReadChunk(tx *sqlx.Tx, hash string) ([]byte, error) {
	mode, err := GetProjectStorageMode(tx)
	if err != nil {
		return nil, err
	}
	if mode == StorageModeCompact {
		var data []byte
		err = tx.Get(&data, "SELECT data FROM chunk WHERE hash = ?", hash)
		return data, err
	}
	if mode != StorageModeDeflated {
		return nil, fmt.Errorf("storage mode %q is not available", mode)
	}
	var count int
	if err := tx.Get(&count, "SELECT COUNT(*) FROM chunk_ref WHERE hash = ?", hash); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, sql.ErrNoRows
	}
	path, _, err := deflatedChunkPath(tx, hash)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func chunkExistsInStore(tx *sqlx.Tx, hash string) (bool, error) {
	mode, err := GetProjectStorageMode(tx)
	if err != nil {
		return false, err
	}
	table := "chunk"
	if mode == StorageModeDeflated {
		table = "chunk_ref"
	} else if mode != StorageModeCompact {
		return false, fmt.Errorf("storage mode %q is not available", mode)
	}
	var count int
	if err := tx.Get(&count, "SELECT COUNT(*) FROM "+table+" WHERE hash = ?", hash); err != nil {
		return false, err
	}
	if count == 0 || mode == StorageModeCompact {
		return count > 0, nil
	}
	path, _, err := deflatedChunkPath(tx, hash)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func chunkInfoFromStore(tx *sqlx.Tx, hash string) (ChunkInfo, error) {
	mode, err := GetProjectStorageMode(tx)
	if err != nil {
		return ChunkInfo{}, err
	}
	var info ChunkInfo
	if mode == StorageModeCompact {
		err = tx.Get(&info, "SELECT hash, size FROM chunk WHERE hash = ?", hash)
	} else if mode == StorageModeDeflated {
		err = tx.Get(&info, "SELECT hash, size FROM chunk_ref WHERE hash = ?", hash)
	} else {
		err = fmt.Errorf("storage mode %q is not available", mode)
	}
	return info, err
}

func DeleteProjectStorage(tx *sqlx.Tx) error {
	mode, err := GetProjectStorageMode(tx)
	if err != nil || mode == StorageModeCompact {
		return err
	}
	if mode != StorageModeDeflated {
		return fmt.Errorf("storage mode %q is not available", mode)
	}
	id, err := projectID(tx)
	if err != nil {
		return err
	}
	storageConfigMu.RLock()
	root := storageRoot
	storageConfigMu.RUnlock()
	if root == "" {
		return errors.New("deflated storage is not configured")
	}
	return os.RemoveAll(filepath.Join(root, id))
}
