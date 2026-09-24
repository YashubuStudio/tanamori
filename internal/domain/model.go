package domain

import "time"

type FolderStatus string

const (
	FolderManaged      FolderStatus = "managed"
	FolderMissing      FolderStatus = "missing"
	FolderUnregistered FolderStatus = "unregistered"
)

type FileStatus string

const (
	FileActive  FileStatus = "active"
	FileMissing FileStatus = "missing"
	FilePending FileStatus = "pending"
)

type ProposalType string

const (
	ProposalMoveFile          ProposalType = "MOVE_FILE"
	ProposalReclassifyFile    ProposalType = "RECLASSIFY_FILE"
	ProposalRegisterFolder    ProposalType = "REGISTER_FOLDER"
	ProposalRemoveFolder      ProposalType = "REMOVE_FOLDER"
	ProposalLinkObservedFile  ProposalType = "LINK_OBSERVED_FILE"
	ProposalPossibleDuplicate ProposalType = "POSSIBLE_DUPLICATE"
)

type ProposalState string

const (
	ProposalPending  ProposalState = "pending"
	ProposalAccepted ProposalState = "accepted"
	ProposalRejected ProposalState = "rejected"
	ProposalExpired  ProposalState = "expired"
	ProposalApplied  ProposalState = "applied"
	ProposalFailed   ProposalState = "failed"
)

type DecisionActor string

const (
	ActorUser               DecisionActor = "USER"
	ActorAIAuto             DecisionActor = "AI_AUTO"
	ActorAIProposalAccepted DecisionActor = "AI_PROPOSAL_ACCEPTED"
	ActorAIProposalRejected DecisionActor = "AI_PROPOSAL_REJECTED"
	ActorSystemRecovery     DecisionActor = "SYSTEM_RECOVERY"
)

type Folder struct {
	ID          string       `json:"folder_id"`
	ParentID    string       `json:"parent_id,omitempty"`
	Name        string       `json:"name"`
	CurrentPath string       `json:"current_path"`
	Kind        string       `json:"kind,omitempty"`
	Purpose     string       `json:"purpose,omitempty"`
	Description string       `json:"description,omitempty"`
	Status      FolderStatus `json:"status"`
	Locked      bool         `json:"locked"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type File struct {
	ID              string     `json:"file_id"`
	CurrentFolderID string     `json:"current_folder_id,omitempty"`
	Name            string     `json:"name"`
	CurrentPath     string     `json:"current_path"`
	SHA256          string     `json:"sha256"`
	Size            int64      `json:"size"`
	MIME            string     `json:"mime,omitempty"`
	ModifiedAt      time.Time  `json:"modified_at"`
	Status          FileStatus `json:"status"`
	ExtractStatus   string     `json:"extract_status"`
	ExtractError    string     `json:"extract_error,omitempty"`
	ObservedAt      time.Time  `json:"observed_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type FileVersion struct {
	ID         string    `json:"version_id"`
	FileID     string    `json:"file_id"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
	ObservedAt time.Time `json:"observed_at"`
}

type ExtractedChunk struct {
	ID              string `json:"chunk_id"`
	FileID          string `json:"file_id"`
	VersionID       string `json:"version_id"`
	Ordinal         int    `json:"ordinal"`
	Text            string `json:"text"`
	FiletextVersion string `json:"filetext_version"`
}

type Proposal struct {
	ID         string        `json:"proposal_id"`
	Type       ProposalType  `json:"type"`
	TargetID   string        `json:"target_id"`
	FromID     string        `json:"from_id,omitempty"`
	ToID       string        `json:"to_id,omitempty"`
	Score      float64       `json:"score"`
	Reason     string        `json:"reason,omitempty"`
	State      ProposalState `json:"state"`
	CreatedAt  time.Time     `json:"created_at"`
	UpdatedAt  time.Time     `json:"updated_at"`
	FileName   string        `json:"file_name,omitempty"`
	FilePath   string        `json:"file_path,omitempty"`
	FolderName string        `json:"folder_name,omitempty"`
	FolderPath string        `json:"folder_path,omitempty"`
}

type Decision struct {
	ID         string        `json:"decision_id"`
	ProposalID string        `json:"proposal_id"`
	Actor      DecisionActor `json:"actor"`
	Action     string        `json:"action"`
	Reason     string        `json:"reason,omitempty"`
	DecidedAt  time.Time     `json:"decided_at"`
}

type Event struct {
	ID            string    `json:"event_id"`
	Type          string    `json:"type"`
	TargetID      string    `json:"target_id,omitempty"`
	Payload       string    `json:"payload,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	CorrelationID string    `json:"correlation_id"`
}

type UserRule struct {
	ID        string    `json:"rule_id"`
	Scope     string    `json:"scope"`
	Condition string    `json:"condition"`
	Action    string    `json:"action"`
	Priority  int       `json:"priority"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ModelRun struct {
	ID           string             `json:"run_id"`
	Task         string             `json:"task"`
	ModelVersion string             `json:"model_version"`
	InputRefs    []string           `json:"input_refs"`
	Scores       map[string]float64 `json:"scores,omitempty"`
	Duration     time.Duration      `json:"duration"`
	Status       string             `json:"status"`
	Error        string             `json:"error,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
}

// ClassificationCandidate is a display-friendly score for one managed folder.
type ClassificationCandidate struct {
	FolderID     string  `json:"folder_id"`
	FolderName   string  `json:"folder_name"`
	Score        float64 `json:"score"`
	Priority     int     `json:"priority,omitempty"`
	ModelVersion string  `json:"model_version,omitempty"`
}

// ClassificationSummary describes the latest persisted classification for a file.
// It is used by local operator interfaces and contains no file body.
type ClassificationSummary struct {
	FileID          string                    `json:"file_id"`
	Name            string                    `json:"name"`
	Path            string                    `json:"path"`
	CurrentFolderID string                    `json:"current_folder_id,omitempty"`
	CurrentFolder   string                    `json:"current_folder,omitempty"`
	ExtractStatus   string                    `json:"extract_status"`
	ExtractError    string                    `json:"extract_error,omitempty"`
	Candidates      []ClassificationCandidate `json:"candidates"`
	Tags            []string                  `json:"tags,omitempty"`
	Top             *ClassificationCandidate  `json:"top,omitempty"`
	ProposalID      string                    `json:"proposal_id,omitempty"`
	ProposalState   ProposalState             `json:"proposal_state,omitempty"`
	CreatedAt       time.Time                 `json:"created_at"`
}

type FileObservation struct {
	Path       string    `json:"path"`
	Name       string    `json:"name"`
	SHA256     string    `json:"sha256"`
	Size       int64     `json:"size"`
	MIME       string    `json:"mime,omitempty"`
	ModifiedAt time.Time `json:"modified_at"`
}

type Extraction struct {
	Format          string   `json:"format"`
	Chunks          []string `json:"chunks"`
	FiletextVersion string   `json:"filetext_version"`
	Truncated       bool     `json:"truncated"`
}

type SearchHit struct {
	FileID  string  `json:"file_id"`
	Name    string  `json:"name"`
	Path    string  `json:"path"`
	Snippet string  `json:"snippet"`
	Rank    float64 `json:"rank"`
}

type MoveOperation struct {
	Proposal Proposal `json:"proposal"`
	File     File     `json:"file"`
	Folder   Folder   `json:"folder"`
}

type ResetResult struct {
	Files   int64 `json:"files"`
	Folders int64 `json:"folders"`
	Jobs    int64 `json:"jobs"`
}
