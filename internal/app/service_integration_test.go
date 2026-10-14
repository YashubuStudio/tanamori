package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/scoring"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

type failingScorer struct{}

type changingExtractor struct{}

type blockingExtractor struct{ started, release chan struct{} }

func (extractor blockingExtractor) Extract(ctx context.Context, path string) (domain.Extraction, error) {
	close(extractor.started)
	select {
	case <-extractor.release:
		return domain.Extraction{Format: "text", Chunks: []string{"契約書"}}, nil
	case <-ctx.Done():
		return domain.Extraction{}, ctx.Err()
	}
}

func (changingExtractor) Extract(ctx context.Context, path string) (domain.Extraction, error) {
	if err := os.WriteFile(path, []byte("changed while extracting"), 0o600); err != nil {
		return domain.Extraction{}, err
	}
	return domain.Extraction{Format: "text", Chunks: []string{"old content"}}, nil
}

func (failingScorer) Score(context.Context, app.ScoreRequest) (app.ScoreResult, error) {
	return app.ScoreResult{}, fmt.Errorf("model unavailable")
}

func TestIngestSearchApproveMoveEndToEnd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "新着契約.txt")
	if err := os.WriteFile(source, []byte("取引先との契約書および更新条件をまとめた資料です。"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if ingested.Proposal == nil {
		t.Fatalf("expected a move proposal: %#v", ingested.Classification)
	}
	pending, err := service.ListProposals(ctx, domain.ProposalPending, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].FileName != "新着契約.txt" || pending[0].FolderName != "契約書" {
		t.Fatalf("proposal names are missing: %#v", pending)
	}
	if pending[0].SourceKind != domain.SourceLocalPath || pending[0].SourceDirectory != root || pending[0].WorkingCopyDirectory != "" || pending[0].DestinationDirectory != destination {
		t.Fatalf("proposal directories are incorrect: %#v", pending[0])
	}
	preview, err := service.PreviewMoveProposal(ctx, pending[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.SourceKind != domain.SourceLocalPath || preview.SourceDirectory != root || preview.WorkingCopyDirectory != "" || preview.DestinationDirectory != destination || preview.SourcePath != source || preview.DestinationPath != filepath.Join(destination, filepath.Base(source)) {
		t.Fatalf("move preview paths are incorrect: %#v", preview)
	}
	hits, err := service.Search(ctx, "契約書", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].FileID != ingested.File.ID {
		t.Fatalf("unexpected search hits: %#v", hits)
	}
	move, err := service.ApplyMoveProposal(ctx, ingested.Proposal.ID, domain.ActorUser, "分類を確認")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
	if data, err := os.ReadFile(move.ToPath); err != nil || len(data) == 0 {
		t.Fatalf("moved file is unavailable: %v", err)
	}
	applied, err := service.ListProposals(ctx, domain.ProposalApplied, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 1 || applied[0].ID != ingested.Proposal.ID {
		t.Fatalf("proposal was not applied: %#v", applied)
	}
}

func TestProposeMoveForUnclassifiedFile(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	accounting := filepath.Join(root, "会計")
	website := filepath.Join(root, "ホームページ")
	for _, directory := range []string{accounting, website} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := filepath.Join(root, "unsorted.txt")
	if err := os.WriteFile(source, []byte("今日の天気とランチの感想についての雑記。"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	accountingFolder, err := service.RegisterFolder(ctx, accounting, "会計", "経費精算、請求書、入金管理", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, website, "ホームページ", "サイト制作、進行管理", ""); err != nil {
		t.Fatal(err)
	}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if ingested.Proposal != nil || ingested.Classification.Action != domain.ClassificationUnclassified {
		t.Fatalf("expected the file to be left unclassified: %#v", ingested.Classification)
	}
	proposal, err := service.ProposeMove(ctx, ingested.File.ID, accountingFolder.ID, 0.42, "テスト用の手動選択")
	if err != nil {
		t.Fatal(err)
	}
	if proposal.FileName != "unsorted.txt" || proposal.FolderName != "会計" || proposal.Score != 0.42 ||
		proposal.State != domain.ProposalPending || proposal.DestinationDirectory != accounting {
		t.Fatalf("unexpected manual proposal: %#v", proposal)
	}
	if _, err := service.ProposeMove(ctx, ingested.File.ID, accountingFolder.ID, 0.1, ""); err == nil {
		t.Fatal("expected a second proposal for the same pending file to be rejected")
	}
	if _, err := service.ProposeMove(ctx, "fil_missing", accountingFolder.ID, 0.1, ""); err == nil {
		t.Fatal("expected proposing for an unknown file to fail")
	}
	move, err := service.ApplyMoveProposal(ctx, proposal.ID, domain.ActorUser, "手動で承認")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(move.ToPath); err != nil {
		t.Fatalf("expected moved file at %q: %v", move.ToPath, err)
	}
	if _, err := service.ProposeMove(ctx, ingested.File.ID, accountingFolder.ID, 0.9, ""); err == nil {
		t.Fatal("expected proposing the folder the file is already in to fail")
	}
}

func TestResetIndexedDataKeepsPhysicalFilesAndSettings(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "新着契約.txt")
	if err := os.WriteFile(source, []byte("契約書と更新条件"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, source); err != nil {
		t.Fatal(err)
	}
	settings := domain.DefaultSettings()
	settings.Priorities["会議"] = 120
	if err := service.SaveSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}
	result, err := service.ResetData(ctx, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.BackupPath == "" {
		t.Fatal("reset did not report a backup")
	}
	restoredPath := filepath.Join(root, "restored.db")
	if err := storage.RestoreBackup(ctx, result.BackupPath, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if folders, err := restored.ListFolders(ctx); err != nil || len(folders) != 1 {
		t.Fatalf("folder was not restored: %#v, %v", folders, err)
	}
	if hits, err := restored.Search(ctx, "契約書", 10); err != nil || len(hits) != 1 {
		t.Fatalf("document was not restored: %#v, %v", hits, err)
	}
	if result.Files != 1 || result.Folders != 1 {
		t.Fatalf("reset counts: %#v", result)
	}
	if folders, err := service.ListFolders(ctx); err != nil || len(folders) != 0 {
		t.Fatalf("folders remain: %#v, %v", folders, err)
	}
	if hits, err := service.Search(ctx, "契約書", 10); err != nil || len(hits) != 0 {
		t.Fatalf("index remains: %#v, %v", hits, err)
	}
	if data, err := os.ReadFile(source); err != nil || len(data) == 0 {
		t.Fatalf("source was removed: %v", err)
	}
	got, err := service.GetSettings(ctx)
	if err != nil || got.Priorities["会議"] != 120 {
		t.Fatalf("settings were reset: %#v, %v", got, err)
	}
}

func TestResetWaitsForIngestBeforeBackingUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "new.txt")
	if err := os.WriteFile(source, []byte("契約書"), 0o600); err != nil {
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
	extractor := blockingExtractor{started: make(chan struct{}), release: make(chan struct{})}
	service, err := app.NewService(db, extractor, scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	ingestDone := make(chan error, 1)
	go func() { _, err := service.Ingest(ctx, source); ingestDone <- err }()
	select {
	case <-extractor.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	resetDone := make(chan struct {
		result domain.ResetResult
		err    error
	}, 1)
	go func() {
		result, err := service.ResetData(ctx, true, true)
		resetDone <- struct {
			result domain.ResetResult
			err    error
		}{result, err}
	}()
	select {
	case <-resetDone:
		t.Fatal("reset ran while ingestion was still active")
	case <-time.After(50 * time.Millisecond):
	}
	close(extractor.release)
	if err := <-ingestDone; err != nil {
		t.Fatal(err)
	}
	reset := <-resetDone
	if reset.err != nil || reset.result.Files != 1 {
		t.Fatalf("reset: %+v %v", reset.result, reset.err)
	}
	restoredPath := filepath.Join(root, "restored.db")
	if err := storage.RestoreBackup(ctx, reset.result.BackupPath, restoredPath); err != nil {
		t.Fatal(err)
	}
	restored, err := storage.Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if hits, err := restored.Search(ctx, "契約書", 10); err != nil || len(hits) != 1 {
		t.Fatalf("in-flight ingestion missing from backup: %#v %v", hits, err)
	}
}

func TestMoveConflictPreviewAndSkip(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "新着契約書.txt"
	source := filepath.Join(root, name)
	if err := os.WriteFile(source, []byte("新しい契約書"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(destination, name), []byte("既存の契約書"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if ingested.Proposal == nil {
		t.Fatal("expected proposal")
	}
	preview, err := service.PreviewMoveProposal(ctx, ingested.Proposal.ID)
	if err != nil || !preview.Conflict || preview.FileName != name {
		t.Fatalf("preview: %#v, %v", preview, err)
	}
	if err := service.RejectMoveProposal(ctx, ingested.Proposal.ID, "同名なので保存しない"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("skip removed source: %v", err)
	}
	rejected, err := service.ListProposals(ctx, domain.ProposalRejected, 10)
	if err != nil || len(rejected) != 1 {
		t.Fatalf("rejection was not saved: %#v, %v", rejected, err)
	}
}

func TestOverwriteIndexedDestinationKeepsDatabaseConsistent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	name := "契約書.txt"
	existing := filepath.Join(destination, name)
	if err := os.WriteFile(existing, []byte("古い契約書"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, name)
	if err := os.WriteFile(source, []byte("新しい契約書"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, existing); err != nil {
		t.Fatal(err)
	}
	newFile, err := service.Ingest(ctx, source)
	if err != nil || newFile.Proposal == nil {
		t.Fatalf("source ingest: %#v, %v", newFile, err)
	}
	if _, err := service.ApplyMoveProposal(ctx, newFile.Proposal.ID, domain.ActorUser, "明示的に上書き", app.MoveOptions{OnConflict: "overwrite"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "新しい契約書" {
		t.Fatalf("destination: %q, %v", data, err)
	}
	hits, err := service.Search(ctx, "古い契約書", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("old search index remains: %#v, %v", hits, err)
	}
}

func TestIngestDoesNotSaveFileChangedDuringExtraction(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "changing.txt")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
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
	service, err := app.NewService(db, changingExtractor{}, scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, path); err == nil {
		t.Fatal("changed file was accepted")
	}
	hits, err := service.Search(ctx, "old", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("changed file was saved: %#v, %v", hits, err)
	}
}
func TestIngestContinuesWhenScorerFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "documents")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "readme.txt")
	if err := os.WriteFile(source, []byte("search remains available"), 0o600); err != nil {
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
	service, err := app.NewService(db, extract.DefaultAdapter(), failingScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterFolder(ctx, destination, "documents", "documents", ""); err != nil {
		t.Fatal(err)
	}
	result, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Classification.Action != domain.ClassificationUnclassified || result.Proposal != nil {
		t.Fatalf("unexpected fallback result: %#v", result)
	}
	if len(result.ModelRuns) != 1 || result.ModelRuns[0].Status != "failed" || result.ModelRuns[0].Error != "model unavailable" {
		t.Fatalf("model failure is not visible: %#v", result.ModelRuns)
	}
	reingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if reingested.File.ID != result.File.ID {
		t.Fatalf("reingest changed file ID: first=%s second=%s", result.File.ID, reingested.File.ID)
	}
	if len(reingested.ModelRuns) != 1 || len(reingested.ModelRuns[0].InputRefs) == 0 || reingested.ModelRuns[0].InputRefs[0] != reingested.File.ID {
		t.Fatalf("model run references stale file ID: file=%s runs=%#v", reingested.File.ID, reingested.ModelRuns)
	}
	hits, err := service.Search(ctx, "available", 10)
	if err != nil || len(hits) != 1 {
		t.Fatalf("FTS fallback failed: %#v, %v", hits, err)
	}
}
