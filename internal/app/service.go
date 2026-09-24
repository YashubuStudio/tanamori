package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

type Service struct {
	repository Repository
	extractor  Extractor
	scorer     Scorer
	filesystem Filesystem
	policy     domain.ClassificationPolicy
	now        func() time.Time
}

func NewService(repository Repository, extractor Extractor, scorer Scorer, filesystem Filesystem, policy domain.ClassificationPolicy) (*Service, error) {
	if repository == nil || extractor == nil || scorer == nil || filesystem == nil {
		return nil, fmt.Errorf("repository, extractor, scorer, and filesystem are required")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Service{
		repository: repository,
		extractor:  extractor,
		scorer:     scorer,
		filesystem: filesystem,
		policy:     policy,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (service *Service) RegisterFolder(ctx context.Context, path, name, purpose, description string) (domain.Folder, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("resolve folder path: %w", err)
	}
	info, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("resolve folder: %w", err)
	}
	stat, err := os.Stat(info)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("inspect folder: %w", err)
	}
	if !stat.IsDir() {
		return domain.Folder{}, fmt.Errorf("not a directory: %s", info)
	}
	if name = strings.TrimSpace(name); name == "" {
		name = filepath.Base(info)
	}
	id, err := domain.NewID("fld")
	if err != nil {
		return domain.Folder{}, err
	}
	now := service.now()
	folder := domain.Folder{
		ID: id, Name: name, CurrentPath: filepath.Clean(info), Purpose: strings.TrimSpace(purpose),
		Description: strings.TrimSpace(description), Status: domain.FolderManaged, CreatedAt: now, UpdatedAt: now,
	}
	folder, err = service.repository.RegisterFolder(ctx, folder)
	if err != nil {
		return domain.Folder{}, err
	}
	return folder, nil
}

func (service *Service) Ingest(ctx context.Context, path string) (IngestResult, error) {
	observation, err := service.filesystem.Inspect(ctx, path)
	if err != nil {
		return IngestResult{}, err
	}
	now := service.now()
	fileID, err := domain.NewID("fil")
	if err != nil {
		return IngestResult{}, err
	}
	versionID, err := domain.NewID("ver")
	if err != nil {
		return IngestResult{}, err
	}
	correlationID, err := domain.NewID("cor")
	if err != nil {
		return IngestResult{}, err
	}

	extraction, extractErr := service.extractor.Extract(ctx, observation.Path)
	extractStatus := "ok"
	extractError := ""
	if extractErr != nil {
		extractStatus = "failed"
		extractError = extractErr.Error()
		extraction = domain.Extraction{}
	}

	file := domain.File{
		ID: fileID, Name: observation.Name, CurrentPath: observation.Path, SHA256: observation.SHA256,
		Size: observation.Size, MIME: observation.MIME, ModifiedAt: observation.ModifiedAt,
		Status: domain.FileActive, ExtractStatus: extractStatus, ExtractError: extractError,
		ObservedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	version := domain.FileVersion{
		ID: versionID, FileID: fileID, SHA256: observation.SHA256, Size: observation.Size,
		ModifiedAt: observation.ModifiedAt, ObservedAt: now,
	}
	chunks := make([]domain.ExtractedChunk, 0, len(extraction.Chunks))
	for ordinal, text := range extraction.Chunks {
		chunkID, idErr := domain.NewID("chk")
		if idErr != nil {
			return IngestResult{}, idErr
		}
		chunks = append(chunks, domain.ExtractedChunk{
			ID: chunkID, FileID: fileID, VersionID: versionID, Ordinal: ordinal,
			Text: text, FiletextVersion: extraction.FiletextVersion,
		})
	}

	folders, err := service.repository.ListFolders(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	settings, err := service.repository.LoadSettings(ctx)
	if err != nil {
		return IngestResult{}, err
	}
	file.CurrentFolderID = containingFolder(observation.Path, folders)
	bodyText := strings.TrimSpace(strings.Join(extraction.Chunks, "\n"))
	leftText := "ファイル名: " + observation.Name
	if bodyText != "" {
		leftText += "\n本文:\n" + bodyText
	}
	tags := documentTags(observation.Name, bodyText, folders)
	candidates := make([]domain.CandidateScore, 0, len(folders))
	modelRuns := make([]domain.ModelRun, 0, len(folders))
	for _, folder := range folders {
		if folder.Status != domain.FolderManaged || folder.Locked {
			continue
		}
		task := "classify_file_to_folder"
		score, scoreErr := service.scorer.Score(ctx, ScoreRequest{
			Task: task, LeftText: leftText,
			RightText: strings.TrimSpace(folder.Name + "\n" + folder.Purpose + "\n" + folder.Description),
			Metadata:  map[string]string{"file_name": observation.Name, "folder_id": folder.ID},
		})
		runID, idErr := domain.NewID("run")
		if idErr != nil {
			return IngestResult{}, idErr
		}
		if scoreErr != nil {
			modelRuns = append(modelRuns, domain.ModelRun{
				ID: runID, Task: task, InputRefs: []string{fileID, folder.ID}, Status: "failed",
				Error: scoreErr.Error(), CreatedAt: now,
			})
			continue
		}
		priority := tagPriority(folder, tags, settings)
		modelRuns = append(modelRuns, domain.ModelRun{
			ID: runID, Task: task, ModelVersion: score.ModelVersion,
			InputRefs: []string{fileID, folder.ID}, Scores: map[string]float64{"score": score.Score, "priority": float64(priority)},
			Duration: score.Duration, Status: "ok", CreatedAt: now,
		})
		candidates = append(candidates, domain.CandidateScore{FolderID: folder.ID, Score: score.Score, Priority: priority, Reason: score.Reason})
	}
	classification := service.policy.Evaluate(candidates)

	var proposal *domain.Proposal
	if (classification.Action == domain.ClassificationProposal || classification.Action == domain.ClassificationAuto) &&
		classification.Top.FolderID != file.CurrentFolderID {
		proposalID, idErr := domain.NewID("prp")
		if idErr != nil {
			return IngestResult{}, idErr
		}
		proposal = &domain.Proposal{
			ID: proposalID, Type: domain.ProposalMoveFile, TargetID: fileID, FromID: file.CurrentFolderID,
			ToID: classification.Top.FolderID, Score: classification.Top.Score,
			Reason: classification.Top.Reason, State: domain.ProposalPending, CreatedAt: now, UpdatedAt: now,
		}
	}
	payload, _ := json.Marshal(map[string]any{
		"path": observation.Path, "sha256": observation.SHA256, "extract_status": extractStatus,
		"classification": classification.Action,
	})
	eventID, err := domain.NewID("evt")
	if err != nil {
		return IngestResult{}, err
	}
	event := domain.Event{
		ID: eventID, Type: "FILE_INGESTED", TargetID: fileID, Payload: string(payload),
		CreatedAt: now, CorrelationID: correlationID,
	}
	record := IngestionRecord{
		File: file, Version: version, Chunks: chunks, Proposal: proposal, Event: event,
		SearchBody: strings.Join(extraction.Chunks, "\n"), Tags: tags, ModelRuns: modelRuns,
	}
	latest, err := os.Lstat(observation.Path)
	if err != nil {
		return IngestResult{}, fmt.Errorf("inspect before save: %w", err)
	}
	if !latest.Mode().IsRegular() || latest.Size() != observation.Size ||
		!latest.ModTime().UTC().Equal(observation.ModifiedAt) {
		return IngestResult{}, fmt.Errorf("file changed during ingestion: %s", observation.Path)
	}
	file, proposal, err = service.repository.SaveIngestion(ctx, record)
	if err != nil {
		return IngestResult{}, err
	}
	return IngestResult{
		File: file, Extraction: extraction, Classification: classification,
		Proposal: proposal, ModelRuns: modelRuns, CorrelationID: correlationID,
	}, nil
}

func containingFolder(filePath string, folders []domain.Folder) string {
	bestID := ""
	bestLength := -1
	for _, folder := range folders {
		if folder.Status != domain.FolderManaged {
			continue
		}
		relative, err := filepath.Rel(folder.CurrentPath, filePath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			continue
		}
		if len(folder.CurrentPath) > bestLength {
			bestID = folder.ID
			bestLength = len(folder.CurrentPath)
		}
	}
	return bestID
}

func (service *Service) ListFolders(ctx context.Context) ([]domain.Folder, error) {
	return service.repository.ListFolders(ctx)
}

func (service *Service) Search(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	return service.repository.Search(ctx, query, limit)
}

func (service *Service) ListProposals(ctx context.Context, state domain.ProposalState, limit int) ([]domain.Proposal, error) {
	return service.repository.ListProposals(ctx, state, limit)
}

func (service *Service) GetSettings(ctx context.Context) (domain.Settings, error) {
	return service.repository.LoadSettings(ctx)
}

func (service *Service) SaveSettings(ctx context.Context, settings domain.Settings) error {
	return service.repository.SaveSettings(ctx, settings)
}

func (service *Service) ResetData(ctx context.Context, documents, folders bool) (domain.ResetResult, error) {
	return service.repository.ResetData(ctx, documents, folders)
}

func (service *Service) PreviewMoveProposal(ctx context.Context, proposalID string) (MovePreview, error) {
	operation, err := service.repository.GetMoveOperation(ctx, proposalID)
	if err != nil {
		return MovePreview{}, err
	}
	destination := filepath.Join(operation.Folder.CurrentPath, operation.File.Name)
	_, statErr := os.Lstat(destination)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return MovePreview{}, statErr
	}
	return MovePreview{FileName: operation.File.Name, SourcePath: operation.File.CurrentPath,
		FolderName: operation.Folder.Name, DestinationPath: destination, Conflict: statErr == nil}, nil
}

func (service *Service) RejectMoveProposal(ctx context.Context, proposalID, reason string) error {
	return service.repository.RejectMove(ctx, proposalID, domain.ActorUser, reason, service.now())
}

func (service *Service) ApplyMoveProposal(ctx context.Context, proposalID string, actor domain.DecisionActor, reason string, options ...MoveOptions) (MoveResult, error) {
	settings, err := service.repository.LoadSettings(ctx)
	if err != nil {
		return MoveResult{}, err
	}
	choice := MoveOptions{OnConflict: "rename", RenamePattern: settings.RenamePattern}
	if len(options) > 0 {
		if options[0].OnConflict != "" {
			choice.OnConflict = options[0].OnConflict
		}
		if options[0].RenamePattern != "" {
			choice.RenamePattern = options[0].RenamePattern
		}
	}
	if choice.OnConflict != "rename" && choice.OnConflict != "overwrite" {
		return MoveResult{}, fmt.Errorf("on_conflict must be rename or overwrite")
	}
	if err := (domain.Settings{Priorities: settings.Priorities, RenamePattern: choice.RenamePattern}).Validate(); err != nil {
		return MoveResult{}, err
	}
	now := service.now()
	operation, err := service.repository.PrepareMove(ctx, proposalID, actor, reason, now)
	if err != nil {
		return MoveResult{}, err
	}
	move, err := service.filesystem.Move(ctx, operation.File.CurrentPath, operation.Folder.CurrentPath, choice)
	if err != nil {
		markErr := service.repository.FailMove(ctx, operation, err.Error(), service.now())
		if markErr != nil {
			return MoveResult{}, errors.Join(err, markErr)
		}
		return MoveResult{}, err
	}
	if err := service.repository.CompleteMove(ctx, operation, move.ToPath, move.SHA256, service.now()); err != nil {
		return move, fmt.Errorf("file moved but database finalization failed: %w", err)
	}
	return move, nil
}
