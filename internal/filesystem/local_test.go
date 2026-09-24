package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/app"
)

func TestInspectRejectsOversizedFileBeforeHashing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.bin")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(1024); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := (Local{MaxFileBytes: 100}).Inspect(context.Background(), path); err == nil {
		t.Fatal("oversized file was accepted")
	}
}

func TestMoveSameNameUsesNumberWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	destinationDir := filepath.Join(root, "destination")
	if err := os.MkdirAll(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(sourceDir, "file.txt")
	if err := os.WriteFile(source, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destinationDir, "file.txt"), []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (Local{}).Move(context.Background(), source, destinationDir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.ToPath) != "file (1).txt" {
		t.Fatalf("unexpected numbered name: %s", result.ToPath)
	}
	data, readErr := os.ReadFile(filepath.Join(destinationDir, "file.txt"))
	if readErr != nil || string(data) != "existing" {
		t.Fatalf("existing file changed: %q, %v", data, readErr)
	}
}

func TestMoveCustomNumberAndExplicitOverwrite(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file.txt", "file_1.txt"} {
		if err := os.WriteFile(filepath.Join(destination, name), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(root, "file.txt")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (Local{}).Move(context.Background(), source, destination, app.MoveOptions{OnConflict: "rename", RenamePattern: "{name}_{n}{ext}"})
	if err != nil || filepath.Base(result.ToPath) != "file_2.txt" {
		t.Fatalf("custom rename: %#v, %v", result, err)
	}
	source = filepath.Join(root, "file.txt")
	if err := os.WriteFile(source, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = (Local{}).Move(context.Background(), source, destination, app.MoveOptions{OnConflict: "overwrite"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(result.ToPath)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("overwrite failed: %q, %v", data, err)
	}
}

func TestMoveSameVolume(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "file.txt")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (Local{}).Move(context.Background(), source, destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	if data, err := os.ReadFile(result.ToPath); err != nil || string(data) != "content" {
		t.Fatalf("destination invalid: %q, %v", data, err)
	}
}
