package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

// Backup creates a consistent SQLite snapshot beside the application database.
// VACUUM INTO includes the schema and settings and works while the DB uses WAL.
func (db *DB) Backup(ctx context.Context) (string, error) {
	if db.path == ":memory:" || strings.HasPrefix(db.path, "file:") {
		return "", fmt.Errorf("backup requires a filesystem database path")
	}
	directory := filepath.Join(filepath.Dir(db.path), "backups")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	id, err := domain.NewID("bak")
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, "tanamori-"+time.Now().UTC().Format("20060102T150405Z")+"-"+id+".db")
	if err := vacuumInto(ctx, db.sql, path); err != nil {
		return "", fmt.Errorf("backup database: %w", err)
	}
	if err := verifyBackup(ctx, path); err != nil {
		_ = os.Remove(path) // only the new, invalid snapshot
		return "", fmt.Errorf("verify new backup %q: %w", path, err)
	}
	return path, nil
}

// RestoreBackup writes a verified backup to a new path only. It never replaces
// an existing database, so operators can switch --db after inspecting it.
func RestoreBackup(ctx context.Context, backupPath, destination string) error {
	if strings.TrimSpace(backupPath) == "" || strings.TrimSpace(destination) == "" {
		return fmt.Errorf("backup and restore destination are required")
	}
	backup, err := filepath.Abs(strings.TrimSpace(backupPath))
	if err != nil {
		return err
	}
	if err := verifyBackup(ctx, backup); err != nil {
		return err
	}
	target, err := filepath.Abs(strings.TrimSpace(destination))
	if err != nil {
		return err
	}
	if target == backup {
		return fmt.Errorf("restore destination must differ from backup")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	source, err := sql.Open("sqlite", "file:"+filepath.ToSlash(backup)+"?mode=ro")
	if err != nil {
		return err
	}
	defer source.Close()
	if err := vacuumInto(ctx, source, target); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	if err := verifyBackup(ctx, target); err != nil {
		_ = os.Remove(target) // only the newly created restore target
		return fmt.Errorf("verify restored database: %w", err)
	}
	return nil
}

func vacuumInto(ctx context.Context, source *sql.DB, path string) (err error) {
	reserved, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := reserved.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	completed := false
	defer func() {
		if !completed {
			_ = os.Remove(path)
		}
	}()
	if _, err := source.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	completed = true
	return nil
}

func verifyBackup(ctx context.Context, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("backup is not a regular file: %s", path)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check failed: %s", result)
	}
	var schemaVersion int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&schemaVersion); err != nil {
		return fmt.Errorf("not a TANAMORI backup: %w", err)
	}
	return nil
}
