package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

type brokenScorer struct{}

func (brokenScorer) Score(context.Context, app.ScoreRequest) (app.ScoreResult, error) {
	return app.ScoreResult{}, errors.New("inference failed")
}

func TestQueueDoesNotMarkModelFailureSuccessful(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "destination")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "document.txt")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service, err := app.NewService(db, extract.DefaultAdapter(), brokenScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "destination", "documents", ""); err != nil {
		t.Fatal(err)
	}
	fileID, err := ingestQueued(ctx, service, source)
	if fileID == "" || err == nil || !strings.Contains(err.Error(), "model scoring failed") {
		t.Fatalf("model failure was hidden: file=%q error=%v", fileID, err)
	}
}
