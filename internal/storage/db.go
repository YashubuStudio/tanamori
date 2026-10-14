package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	sql  *sql.DB
	path string
}

func Open(ctx context.Context, path string) (*DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path must not be empty")
	}
	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve database path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
		path = absolute
		dsn = "file:" + filepath.ToSlash(absolute)
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection serializes short writes from scanner and file workers.
	// Model inference does not hold the connection, so HTTP reads remain responsive.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(0)
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	db := &DB{sql: sqlDB, path: path}
	if _, err := sqlDB.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if path != ":memory:" {
		if _, err := sqlDB.ExecContext(ctx, "PRAGMA journal_mode = WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}
	return db, nil
}

func (db *DB) Path() string { return db.path }

func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

func (db *DB) Migrate(ctx context.Context) error {
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateJobOrigins(ctx, transaction); err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, jobLogSchema); err != nil {
		return fmt.Errorf("apply job log triggers: %w", err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)", formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record schema migration: %w", err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(2, ?)", formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record tag schema migration: %w", err)
	}
	if _, err := transaction.ExecContext(ctx,
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(3, ?)", formatTime(time.Now().UTC())); err != nil {
		return fmt.Errorf("record upload origin and work log schema migration: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

// Existing queues predate the per-job origin snapshot. Prefer their latest
// recorded event so reusing an incoming path cannot change historical origins.
func migrateJobOrigins(ctx context.Context, tx *sql.Tx) error {
	var present int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('ingest_jobs')
WHERE name = 'source_relative_path'`).Scan(&present); err != nil {
		return fmt.Errorf("inspect job origin schema: %w", err)
	}
	if present == 0 {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE ingest_jobs ADD COLUMN source_relative_path TEXT NOT NULL DEFAULT ''"); err != nil {
			return fmt.Errorf("add job origin snapshot: %w", err)
		}
	}
	var applied int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 4").Scan(&applied); err != nil {
		return err
	}
	if applied == 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE ingest_jobs SET source_relative_path = COALESCE(
    (SELECT NULLIF(json_extract(payload, '$.relative_path'), '') FROM events
     WHERE target_id = ingest_jobs.job_id AND type IN ('JOB_QUEUED','JOB_STARTED','JOB_RETRY','JOB_COMPLETED','JOB_FAILED')
       AND json_valid(payload) AND COALESCE(json_extract(payload, '$.relative_path'), '') != ''
     ORDER BY created_at DESC, event_id DESC LIMIT 1),
    (SELECT relative_path FROM upload_origins WHERE stored_path = ingest_jobs.path), '')`); err != nil {
			return fmt.Errorf("backfill job origins: %w", err)
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(4, ?)", formatTime(time.Now().UTC()))
	return err
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", value, err)
	}
	return parsed, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
