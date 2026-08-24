package repository

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestApplySyncableProjectConfigs(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", ":memory:")
	t.Cleanup(func() { db.Close() })
	db.MustExec(`CREATE TABLE config (name TEXT PRIMARY KEY, value CLOB, mtime INTEGER NOT NULL, synced BOOLEAN DEFAULT 0 NOT NULL)`)
	tx := db.MustBegin()

	err := ApplySyncableProjectConfigs(tx, []ProjectConfig{{
		Name: "project_script_settings_v1", Value: `{"version":1}`, Mtime: 42,
	}})
	require.NoError(t, err)

	configs, err := GetSyncableProjectConfigs(tx, false)
	require.NoError(t, err)
	require.Len(t, configs, 1)
	require.Equal(t, 42, configs[0].Mtime)
	require.True(t, configs[0].Synced)
}

func TestApplySyncableProjectConfigsRejectsUnknownKey(t *testing.T) {
	db := sqlx.MustOpen("sqlite3", ":memory:")
	t.Cleanup(func() { db.Close() })
	db.MustExec(`CREATE TABLE config (name TEXT PRIMARY KEY, value CLOB, mtime INTEGER NOT NULL, synced BOOLEAN DEFAULT 0 NOT NULL)`)
	tx := db.MustBegin()

	err := ApplySyncableProjectConfigs(tx, []ProjectConfig{{Name: "unsafe", Value: "value", Mtime: 1}})
	require.ErrorContains(t, err, "not syncable")
}
