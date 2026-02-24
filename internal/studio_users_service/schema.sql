-- Studio Users Database Schema
-- This schema is for studio-level authentication (login/registration)
-- NOT for per-project permissions (those are in internal/repository/schema.sql)

CREATE TABLE IF NOT EXISTS role (
    id TEXT PRIMARY KEY NOT NULL,
    mtime INTEGER NOT NULL DEFAULT 0,
    name TEXT NOT NULL UNIQUE COLLATE NOCASE
);

CREATE TABLE IF NOT EXISTS user (
    id TEXT PRIMARY KEY,
    mtime INTEGER NOT NULL DEFAULT 0,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    username TEXT NOT NULL UNIQUE COLLATE NOCASE,
    email TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password TEXT NOT NULL,
    last_presence DATETIME DEFAULT CURRENT_TIMESTAMP,
    login_failed_attempts INTEGER NOT NULL DEFAULT 0,
    last_login_failed DATETIME DEFAULT NULL,
    totp_enabled BOOLEAN NOT NULL DEFAULT 0,
    totp_secret TEXT DEFAULT NULL,
    email_otp_enabled BOOLEAN NOT NULL DEFAULT 0,
    email_otp_secret TEXT DEFAULT NULL,
    photo BLOB,
    has_avatar BOOLEAN NOT NULL DEFAULT 0,
    added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    active BOOLEAN DEFAULT 1,
    is_deleted BOOLEAN DEFAULT 0,
    role_id TEXT REFERENCES role(id)
);

CREATE INDEX IF NOT EXISTS idx_user_email ON user(email);
CREATE INDEX IF NOT EXISTS idx_user_username ON user(username);
