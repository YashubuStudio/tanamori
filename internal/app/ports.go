package app

import (
	"context"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

type Repository interface {
	RegisterFolder(context.Context, domain.Folder) (domain.Folder, error)
	ListFolders(context.Context) ([]domain.Folder, error)
	LoadSettings(context.Context) (domain.Settings, error)
	SaveSettings(context.Context, domain.Settings) error
	PreviewReset(context.Context) (domain.ResetPreview, error)
	ResetData(context.Context, bool, bool) (domain.ResetResult, error)
	SaveIngestion(context.Context, IngestionRecord) (domain.File, *domain.Proposal, error)
	Search(context.Context, string, int) ([]domain.SearchHit, error)
	ListProposals(context.Context, domain.ProposalState, int) ([]domain.Proposal, error)
	PrepareMove(context.Context, string, domain.DecisionActor, string, time.Time) (domain.MoveOperation, error)
	GetMoveOperation(context.Context, string) (domain.MoveOperation, error)
	RejectMove(context.Context, string, domain.DecisionActor, string, time.Time) error
	CompleteMove(context.Context, domain.MoveOperation, string, string, time.Time) error
	FailMove(context.Context, domain.MoveOperation, string, time.Time) error
	DeleteUploadOrigin(context.Context, string) error
	CreateManualProposal(ctx context.Context, fileID, folderID string, score float64, reason string, now time.Time) (domain.Proposal, error)
}

type Extractor interface {
	Extract(context.Context, string) (domain.Extraction, error)
}

type Scorer interface {
	Score(context.Context, ScoreRequest) (ScoreResult, error)
}

type Filesystem interface {
	Inspect(context.Context, string) (domain.FileObservation, error)
	Move(context.Context, string, string, ...MoveOptions) (MoveResult, error)
}

type MoveOptions struct {
	OnConflict    string `json:"on_conflict"` // rename or overwrite
	RenamePattern string `json:"rename_pattern,omitempty"`
}

type MovePreview struct {
	FileName             string `json:"file_name"`
	SourcePath           string `json:"source_path"`
	SourceDirectory      string `json:"source_directory"`
	SourceKind           string `json:"source_kind"`
	SourceRelativePath   string `json:"source_relative_path,omitempty"`
	WorkingCopyDirectory string `json:"working_copy_directory,omitempty"`
	FolderName           string `json:"folder_name"`
	DestinationPath      string `json:"destination_path"`
	DestinationDirectory string `json:"destination_directory"`
	Conflict             bool   `json:"conflict"`
}

type ScoreRequest struct {
	Task      string            `json:"task"`
	LeftText  string            `json:"left_text"`
	RightText string            `json:"right_text"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type ScoreResult struct {
	Score        float64       `json:"score"`
	Reason       string        `json:"reason,omitempty"`
	ModelVersion string        `json:"model_version"`
	Duration     time.Duration `json:"duration"`
}

type MoveResult struct {
	FromPath string `json:"from_path"`
	ToPath   string `json:"to_path"`
	SHA256   string `json:"sha256"`
}

type IngestionRecord struct {
	File       domain.File
	Version    domain.FileVersion
	Chunks     []domain.ExtractedChunk
	Proposal   *domain.Proposal
	Event      domain.Event
	SearchBody string
	Tags       []string
	ModelRuns  []domain.ModelRun
}

type IngestResult struct {
	File           domain.File                 `json:"file"`
	Extraction     domain.Extraction           `json:"extraction"`
	Classification domain.ClassificationResult `json:"classification"`
	Tags           []string                    `json:"tags,omitempty"`
	Proposal       *domain.Proposal            `json:"proposal,omitempty"`
	ModelRuns      []domain.ModelRun           `json:"model_runs,omitempty"`
	CorrelationID  string                      `json:"correlation_id"`
}
