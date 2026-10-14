package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

func TestBackupRestoreAndReset(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	settings := domain.DefaultSettings()
	settings.Priorities["会議"] = 123
	if err := db.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	result, err := db.ResetData(ctx, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath == "" {
		t.Fatal("reset did not create a backup")
	}
	if err := verifyBackup(ctx, result.BackupPath); err != nil {
		t.Fatal(err)
	}
	restoredPath := filepath.Join(root, "restored.db")
	if err := RestoreBackup(ctx, result.BackupPath, restoredPath); err != nil {
		t.Fatal(err)
	}
	if err := RestoreBackup(ctx, result.BackupPath, restoredPath); err == nil {
		t.Fatal("restore overwrote existing DB")
	}
	restored, err := Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.LoadSettings(ctx)
	if err != nil || got.Priorities["会議"] != 123 {
		t.Fatalf("settings not restored: %+v, %v", got, err)
	}
}

func TestResetRemovesBackupWhenDeleteTransactionFailsAfterBackup(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Drop a table the delete transaction depends on so the backup (which
	// copies whatever schema exists right now) still succeeds, but the
	// later in-transaction DELETE fails well after the backup was made.
	if _, err := db.sql.ExecContext(ctx, "DROP TABLE events"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResetData(ctx, true, true); err == nil {
		t.Fatal("expected reset to fail when a delete statement errors")
	}
	backupsDir := filepath.Join(root, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover backup files after a failed reset, found %v", entries)
	}
}

func TestResetDoesNotDeleteWhenBackupFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backups"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResetData(ctx, true, true); err == nil {
		t.Fatal("reset succeeded without backup")
	}
	if _, err := db.sql.ExecContext(ctx, "SELECT 1 FROM settings"); err != nil {
		t.Fatalf("database was damaged: %v", err)
	}
}
