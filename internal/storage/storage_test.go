package storage

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
)

func TestStorageIngestionSearchAndMoveLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	destinationPath := filepath.Join(t.TempDir(), "manuals")
	folder, err := db.RegisterFolder(ctx, domain.Folder{
		ID: "fld_manuals", Name: "Manuals", CurrentPath: destinationPath,
		Purpose: "manual documentation", Status: domain.FolderManaged,
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "guide.txt")
	proposal := &domain.Proposal{
		ID: "prp_move", Type: domain.ProposalMoveFile, TargetID: "fil_1", ToID: folder.ID,
		Score: 0.8, Reason: "manual match", State: domain.ProposalPending,
		CreatedAt: now, UpdatedAt: now,
	}
	file, savedProposal, err := db.SaveIngestion(ctx, app.IngestionRecord{
		File: domain.File{
			ID: "fil_1", Name: "guide.txt", CurrentPath: sourcePath, SHA256: "abc",
			Size: 12, ModifiedAt: now, Status: domain.FileActive, ExtractStatus: "ok",
			ObservedAt: now, CreatedAt: now, UpdatedAt: now,
		},
		Version: domain.FileVersion{
			ID: "ver_1", FileID: "fil_1", SHA256: "abc", Size: 12, ModifiedAt: now, ObservedAt: now,
		},
		Chunks: []domain.ExtractedChunk{{
			ID: "chk_1", FileID: "fil_1", VersionID: "ver_1", Text: "searchable operator manual 日本語の契約書です",
			FiletextVersion: "test",
		}},
		Proposal: proposal,
		Event: domain.Event{
			ID: "evt_1", Type: "FILE_INGESTED", TargetID: "fil_1", Payload: "{}",
			CreatedAt: now, CorrelationID: "cor_1",
		},
		SearchBody: "searchable operator manual 日本語の契約書です",
		ModelRuns: []domain.ModelRun{{
			ID: "run_1", Task: "classify_file_to_folder", ModelVersion: "test-v1",
			InputRefs: []string{"fil_1", folder.ID}, Scores: map[string]float64{"score": 0.8},
			Status: "ok", CreatedAt: now,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if file.ID != "fil_1" || savedProposal == nil {
		t.Fatalf("unexpected save result: %#v %#v", file, savedProposal)
	}
	var modelRunCount int
	if err := db.sql.QueryRowContext(ctx, "SELECT COUNT(*) FROM model_runs WHERE run_id = 'run_1' AND status = 'ok'").Scan(&modelRunCount); err != nil {
		t.Fatal(err)
	}
	if modelRunCount != 1 {
		t.Fatalf("model run count = %d", modelRunCount)
	}
	hits, err := db.Search(ctx, "operator manual", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].FileID != file.ID {
		t.Fatalf("unexpected search hits: %#v", hits)
	}
	japaneseHits, err := db.Search(ctx, "契約書", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(japaneseHits) != 1 || !strings.Contains(japaneseHits[0].Snippet, "契約書") {
		t.Fatalf("unexpected Japanese search hits: %#v", japaneseHits)
	}
	operation, err := db.PrepareMove(ctx, proposal.ID, domain.ActorUser, "approved", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(destinationPath, "guide.txt")
	if err := db.CompleteMove(ctx, operation, newPath, "abc", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	applied, err := db.ListProposals(ctx, domain.ProposalApplied, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].ID != proposal.ID {
		t.Fatalf("unexpected applied proposals: %#v", applied)
	}
	hits, err = db.Search(ctx, "operator", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Path != newPath {
		t.Fatalf("search path was not updated: %#v", hits)
	}
}

func TestRegisterFolderIsIdempotentByPath(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "folder")
	first, err := db.RegisterFolder(ctx, domain.Folder{ID: "fld_first", Name: "A", CurrentPath: path, Status: domain.FolderManaged, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.RegisterFolder(ctx, domain.Folder{ID: "fld_second", Name: "B", CurrentPath: path, Status: domain.FolderManaged, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Name != "B" {
		t.Fatalf("folder identity changed: %#v %#v", first, second)
	}
}
