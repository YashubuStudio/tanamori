package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

// RecordUploadOrigin records the browser-visible relative path before a queued
// worker can ingest the durable upload copy. Browsers do not expose the
// original absolute path.
func (db *DB) RecordUploadOrigin(ctx context.Context, storedPath, relativePath string) error {
	_, err := db.sql.ExecContext(ctx, `INSERT INTO upload_origins(stored_path, relative_path, created_at)
VALUES(?, ?, ?)`, storedPath, filepath.ToSlash(relativePath), formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("record upload origin: %w", err)
	}
	return nil
}

func (db *DB) DeleteUploadOrigin(ctx context.Context, storedPath string) error {
	_, err := db.sql.ExecContext(ctx, "DELETE FROM upload_origins WHERE stored_path = ?", storedPath)
	return err
}

func (db *DB) uploadRelativePath(ctx context.Context, storedPath string) (string, error) {
	var relative string
	err := db.sql.QueryRowContext(ctx, "SELECT relative_path FROM upload_origins WHERE stored_path = ?", storedPath).Scan(&relative)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load upload origin: %w", err)
	}
	return relative, nil
}

func (db *DB) sourceKind(path, relativePath string) string {
	if relativePath != "" || db.isLegacyIncomingCopy(path) {
		return domain.SourceBrowserUpload
	}
	return domain.SourceLocalPath
}

func (db *DB) isLegacyIncomingCopy(path string) bool {
	if path == "" || db.path == ":memory:" {
		return false
	}
	incoming := filepath.Join(filepath.Dir(db.path), "incoming")
	relative, err := filepath.Rel(incoming, path)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
