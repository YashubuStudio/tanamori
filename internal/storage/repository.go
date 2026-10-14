package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
)

type scanner interface {
	Scan(...any) error
}

func (db *DB) RegisterFolder(ctx context.Context, folder domain.Folder) (domain.Folder, error) {
	row := db.sql.QueryRowContext(ctx, `
INSERT INTO folders(folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(current_path) DO UPDATE SET
    parent_id=excluded.parent_id,
    name=excluded.name,
    kind=excluded.kind,
    purpose=excluded.purpose,
    description=excluded.description,
    status=excluded.status,
    locked=excluded.locked,
    updated_at=excluded.updated_at
RETURNING folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at`,
		folder.ID, nullable(folder.ParentID), folder.Name, folder.CurrentPath, folder.Kind,
		folder.Purpose, folder.Description, folder.Status, folder.Locked,
		formatTime(folder.CreatedAt), formatTime(folder.UpdatedAt))
	registered, err := scanFolder(row)
	if err != nil {
		return domain.Folder{}, fmt.Errorf("register folder: %w", err)
	}
	return registered, nil
}

func (db *DB) ListFolders(ctx context.Context) ([]domain.Folder, error) {
	rows, err := db.sql.QueryContext(ctx, `
SELECT folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at
FROM folders ORDER BY current_path`)
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	defer rows.Close()
	folders := make([]domain.Folder, 0)
	for rows.Next() {
		folder, err := scanFolder(rows)
		if err != nil {
			return nil, fmt.Errorf("scan folder: %w", err)
		}
		folders = append(folders, folder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate folders: %w", err)
	}
	return folders, nil
}

func (db *DB) SaveIngestion(ctx context.Context, record app.IngestionRecord) (domain.File, *domain.Proposal, error) {
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.File{}, nil, fmt.Errorf("begin ingestion: %w", err)
	}
	defer transaction.Rollback()

	originalFileID := record.File.ID
	var existingID, existingCreated string
	err = transaction.QueryRowContext(ctx,
		"SELECT file_id, created_at FROM files WHERE current_path = ?", record.File.CurrentPath).Scan(&existingID, &existingCreated)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.File{}, nil, fmt.Errorf("find existing file: %w", err)
	}
	if err == nil {
		record.File.ID = existingID
		createdAt, parseErr := parseTime(existingCreated)
		if parseErr != nil {
			return domain.File{}, nil, parseErr
		}
		record.File.CreatedAt = createdAt
		record.Version.FileID = existingID
		for index := range record.Chunks {
			record.Chunks[index].FileID = existingID
		}
		for runIndex := range record.ModelRuns {
			for refIndex, reference := range record.ModelRuns[runIndex].InputRefs {
				if reference == originalFileID {
					record.ModelRuns[runIndex].InputRefs[refIndex] = existingID
				}
			}
		}
		record.Event.TargetID = existingID
	}
	var proposal *domain.Proposal
	if record.Proposal != nil {
		copy := *record.Proposal
		copy.TargetID = record.File.ID
		proposal = &copy
	}

	_, err = transaction.ExecContext(ctx, `
INSERT INTO files(file_id, current_folder_id, name, current_path, sha256, size, mime, modified_at, status,
                  extract_status, extract_error, observed_at, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(file_id) DO UPDATE SET
    current_folder_id=excluded.current_folder_id,
    name=excluded.name,
    current_path=excluded.current_path,
    sha256=excluded.sha256,
    size=excluded.size,
    mime=excluded.mime,
    modified_at=excluded.modified_at,
    status=excluded.status,
    extract_status=excluded.extract_status,
    extract_error=excluded.extract_error,
    observed_at=excluded.observed_at,
    updated_at=excluded.updated_at`,
		record.File.ID, nullable(record.File.CurrentFolderID), record.File.Name, record.File.CurrentPath,
		record.File.SHA256, record.File.Size, record.File.MIME, formatTime(record.File.ModifiedAt), record.File.Status,
		record.File.ExtractStatus, record.File.ExtractError, formatTime(record.File.ObservedAt),
		formatTime(record.File.CreatedAt), formatTime(record.File.UpdatedAt))
	if err != nil {
		return domain.File{}, nil, fmt.Errorf("save file: %w", err)
	}
	if _, err = transaction.ExecContext(ctx, "DELETE FROM file_tags WHERE file_id = ?", record.File.ID); err != nil {
		return domain.File{}, nil, fmt.Errorf("clear file tags: %w", err)
	}
	for _, tag := range record.Tags {
		if _, err = transaction.ExecContext(ctx, "INSERT INTO file_tags(file_id, tag) VALUES(?, ?)", record.File.ID, tag); err != nil {
			return domain.File{}, nil, fmt.Errorf("save file tag: %w", err)
		}
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO file_versions(version_id, file_id, sha256, size, modified_at, observed_at)
VALUES(?, ?, ?, ?, ?, ?)`, record.Version.ID, record.File.ID, record.Version.SHA256,
		record.Version.Size, formatTime(record.Version.ModifiedAt), formatTime(record.Version.ObservedAt))
	if err != nil {
		return domain.File{}, nil, fmt.Errorf("save file version: %w", err)
	}
	for _, chunk := range record.Chunks {
		_, err = transaction.ExecContext(ctx, `
INSERT INTO extracted_chunks(chunk_id, file_id, version_id, ordinal, text, filetext_version)
VALUES(?, ?, ?, ?, ?, ?)`, chunk.ID, record.File.ID, record.Version.ID, chunk.Ordinal, chunk.Text, chunk.FiletextVersion)
		if err != nil {
			return domain.File{}, nil, fmt.Errorf("save extracted chunk %d: %w", chunk.Ordinal, err)
		}
	}
	if proposal != nil {
		_, err = transaction.ExecContext(ctx, `
INSERT INTO proposals(proposal_id, type, target_id, from_id, to_id, score, reason, state, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, proposal.ID, proposal.Type, proposal.TargetID,
			proposal.FromID, proposal.ToID, proposal.Score, proposal.Reason, proposal.State,
			formatTime(proposal.CreatedAt), formatTime(proposal.UpdatedAt))
		if err != nil {
			return domain.File{}, nil, fmt.Errorf("save proposal: %w", err)
		}
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO events(event_id, type, target_id, payload, created_at, correlation_id)
VALUES(?, ?, ?, ?, ?, ?)`, record.Event.ID, record.Event.Type, record.File.ID, record.Event.Payload,
		formatTime(record.Event.CreatedAt), record.Event.CorrelationID)
	if err != nil {
		return domain.File{}, nil, fmt.Errorf("save ingestion event: %w", err)
	}
	folderDescription := ""
	if record.File.CurrentFolderID != "" {
		_ = transaction.QueryRowContext(ctx,
			"SELECT purpose || ' ' || description FROM folders WHERE folder_id = ?", record.File.CurrentFolderID).Scan(&folderDescription)
	}
	if _, err = transaction.ExecContext(ctx, "DELETE FROM file_search WHERE file_id = ?", record.File.ID); err != nil {
		return domain.File{}, nil, fmt.Errorf("clear search document: %w", err)
	}
	if _, err = transaction.ExecContext(ctx, `
INSERT INTO file_search(file_id, name, path, body, tags, folder_description, raw_name, raw_path, raw_body)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, record.File.ID,
		augmentForFTS(record.File.Name), augmentForFTS(record.File.CurrentPath), augmentForFTS(record.SearchBody),
		augmentForFTS(strings.Join(record.Tags, " ")), augmentForFTS(folderDescription), record.File.Name, record.File.CurrentPath, record.SearchBody); err != nil {
		return domain.File{}, nil, fmt.Errorf("index file: %w", err)
	}
	for _, run := range record.ModelRuns {
		inputRefs, marshalErr := json.Marshal(run.InputRefs)
		if marshalErr != nil {
			return domain.File{}, nil, fmt.Errorf("encode model run input refs: %w", marshalErr)
		}
		scores, marshalErr := json.Marshal(run.Scores)
		if marshalErr != nil {
			return domain.File{}, nil, fmt.Errorf("encode model run scores: %w", marshalErr)
		}
		_, err = transaction.ExecContext(ctx, `
INSERT INTO model_runs(run_id, task, model_version, input_refs, scores, duration_ms, status, error, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.Task, run.ModelVersion, string(inputRefs), string(scores),
			run.Duration.Milliseconds(), run.Status, run.Error, formatTime(run.CreatedAt))
		if err != nil {
			return domain.File{}, nil, fmt.Errorf("save model run: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return domain.File{}, nil, fmt.Errorf("commit ingestion: %w", err)
	}
	return record.File, proposal, nil
}

func (db *DB) ListProposals(ctx context.Context, state domain.ProposalState, limit int) ([]domain.Proposal, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	query := `SELECT p.proposal_id, p.type, p.target_id, p.from_id, p.to_id, p.score, p.reason, p.state, p.created_at, p.updated_at,
COALESCE(f.name, ''), COALESCE(f.current_path, ''), COALESCE(d.name, ''), COALESCE(d.current_path, ''),
COALESCE(u.relative_path, '')
FROM proposals p LEFT JOIN files f ON f.file_id = p.target_id LEFT JOIN folders d ON d.folder_id = p.to_id
LEFT JOIN upload_origins u ON u.stored_path = f.current_path`
	args := []any{}
	if state != "" {
		query += " WHERE p.state = ?"
		args = append(args, state)
	}
	query += " ORDER BY p.created_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer rows.Close()
	proposals := make([]domain.Proposal, 0)
	for rows.Next() {
		proposal, err := scanProposalWithNames(rows)
		if err != nil {
			return nil, err
		}
		proposal.SourceKind = db.sourceKind(proposal.FilePath, proposal.SourceRelativePath)
		if proposal.SourceKind == domain.SourceBrowserUpload {
			proposal.SourceDirectory = ""
			proposal.WorkingCopyDirectory = filepath.Dir(proposal.FilePath)
		} else if proposal.FilePath != "" {
			proposal.SourceDirectory = filepath.Dir(proposal.FilePath)
		}
		proposals = append(proposals, proposal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proposals: %w", err)
	}
	return proposals, nil
}

func scanProposalWithNames(row scanner) (domain.Proposal, error) {
	var proposal domain.Proposal
	var proposalType, state, createdAt, updatedAt string
	if err := row.Scan(&proposal.ID, &proposalType, &proposal.TargetID, &proposal.FromID,
		&proposal.ToID, &proposal.Score, &proposal.Reason, &state, &createdAt, &updatedAt,
		&proposal.FileName, &proposal.FilePath, &proposal.FolderName, &proposal.FolderPath,
		&proposal.SourceRelativePath); err != nil {
		return domain.Proposal{}, err
	}
	proposal.Type = domain.ProposalType(proposalType)
	proposal.State = domain.ProposalState(state)
	var err error
	if proposal.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Proposal{}, err
	}
	if proposal.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.Proposal{}, err
	}
	proposal.DestinationDirectory = proposal.FolderPath
	return proposal, nil
}

func (db *DB) PrepareMove(ctx context.Context, proposalID string, actor domain.DecisionActor, reason string, now time.Time) (domain.MoveOperation, error) {
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("begin move preparation: %w", err)
	}
	defer transaction.Rollback()
	proposal, err := scanProposal(transaction.QueryRowContext(ctx, `
SELECT proposal_id, type, target_id, from_id, to_id, score, reason, state, created_at, updated_at
FROM proposals WHERE proposal_id = ?`, proposalID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load proposal: %w", err)
	}
	if proposal.State != domain.ProposalPending {
		return domain.MoveOperation{}, fmt.Errorf("proposal %s is %s, not pending", proposal.ID, proposal.State)
	}
	if proposal.Type != domain.ProposalMoveFile && proposal.Type != domain.ProposalReclassifyFile {
		return domain.MoveOperation{}, fmt.Errorf("proposal %s is not a file move", proposal.ID)
	}
	file, err := scanFile(transaction.QueryRowContext(ctx, `
SELECT file_id, current_folder_id, name, current_path, sha256, size, mime, modified_at, status,
       extract_status, extract_error, observed_at, created_at, updated_at
FROM files WHERE file_id = ?`, proposal.TargetID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load proposal file: %w", err)
	}
	folder, err := scanFolder(transaction.QueryRowContext(ctx, `
SELECT folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at
FROM folders WHERE folder_id = ?`, proposal.ToID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load destination folder: %w", err)
	}
	if folder.Status != domain.FolderManaged || folder.Locked {
		return domain.MoveOperation{}, fmt.Errorf("destination folder is not writable by policy")
	}
	if _, err := transaction.ExecContext(ctx,
		"UPDATE proposals SET state = ?, updated_at = ? WHERE proposal_id = ? AND state = ?",
		domain.ProposalAccepted, formatTime(now), proposal.ID, domain.ProposalPending); err != nil {
		return domain.MoveOperation{}, fmt.Errorf("accept proposal: %w", err)
	}
	decisionID, err := domain.NewID("dec")
	if err != nil {
		return domain.MoveOperation{}, err
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO decisions(decision_id, proposal_id, actor, action, reason, decided_at)
VALUES(?, ?, ?, 'accept', ?, ?)`, decisionID, proposal.ID, actor, reason, formatTime(now)); err != nil {
		return domain.MoveOperation{}, fmt.Errorf("save decision: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"from": file.CurrentPath, "to_folder": folder.CurrentPath})
	if err := insertEvent(ctx, transaction, "MOVE_PLANNED", file.ID, string(payload), now); err != nil {
		return domain.MoveOperation{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domain.MoveOperation{}, fmt.Errorf("commit move preparation: %w", err)
	}
	proposal.State = domain.ProposalAccepted
	proposal.UpdatedAt = now
	return domain.MoveOperation{Proposal: proposal, File: file, Folder: folder}, nil
}

func (db *DB) CompleteMove(ctx context.Context, operation domain.MoveOperation, newPath, hash string, now time.Time) error {
	if hash != operation.File.SHA256 {
		return fmt.Errorf("moved file hash changed: expected %s, got %s", operation.File.SHA256, hash)
	}
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move completion: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, `DELETE FROM file_search WHERE file_id IN
(SELECT file_id FROM files WHERE current_path = ? AND file_id <> ?)`, newPath, operation.File.ID); err != nil {
		return fmt.Errorf("clear overwritten file search index: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM model_runs WHERE json_extract(input_refs, '$[0]') IN
(SELECT file_id FROM files WHERE current_path = ? AND file_id <> ?)`, newPath, operation.File.ID); err != nil {
		return fmt.Errorf("clear overwritten file model runs: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `DELETE FROM proposals WHERE target_id IN
(SELECT file_id FROM files WHERE current_path = ? AND file_id <> ?)`, newPath, operation.File.ID); err != nil {
		return fmt.Errorf("clear overwritten file proposals: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM files WHERE current_path = ? AND file_id <> ?", newPath, operation.File.ID); err != nil {
		return fmt.Errorf("clear overwritten file index: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `
UPDATE files SET current_folder_id = ?, current_path = ?, name = ?, status = ?, updated_at = ?
WHERE file_id = ? AND current_path = ? AND sha256 = ?`, operation.Folder.ID, newPath,
		filepathBase(newPath), domain.FileActive, formatTime(now), operation.File.ID,
		operation.File.CurrentPath, operation.File.SHA256)
	if err != nil {
		return fmt.Errorf("update moved file: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("file database state changed before move completion")
	}
	result, err = transaction.ExecContext(ctx,
		"UPDATE proposals SET state = ?, updated_at = ? WHERE proposal_id = ? AND state = ?",
		domain.ProposalApplied, formatTime(now), operation.Proposal.ID, domain.ProposalAccepted)
	if err != nil {
		return fmt.Errorf("complete proposal: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return fmt.Errorf("proposal database state changed before move completion")
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE file_search SET path = ?, raw_path = ?, folder_description = ? WHERE file_id = ?`,
		augmentForFTS(newPath), newPath, augmentForFTS(operation.Folder.Purpose+" "+operation.Folder.Description), operation.File.ID); err != nil {
		return fmt.Errorf("update search path: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"from": operation.File.CurrentPath, "to": newPath, "sha256": hash})
	if err := insertEvent(ctx, transaction, "MOVE_APPLIED", operation.File.ID, string(payload), now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit move completion: %w", err)
	}
	return nil
}

func (db *DB) FailMove(ctx context.Context, operation domain.MoveOperation, failure string, now time.Time) error {
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin move failure record: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx,
		"UPDATE proposals SET state = ?, updated_at = ? WHERE proposal_id = ? AND state = ?",
		domain.ProposalFailed, formatTime(now), operation.Proposal.ID, domain.ProposalAccepted); err != nil {
		return fmt.Errorf("fail proposal: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"error": failure, "from": operation.File.CurrentPath, "to_folder": operation.Folder.CurrentPath})
	if err := insertEvent(ctx, transaction, "MOVE_FAILED", operation.File.ID, string(payload), now); err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit move failure: %w", err)
	}
	return nil
}

func insertEvent(ctx context.Context, transaction *sql.Tx, eventType, targetID, payload string, now time.Time) error {
	eventID, err := domain.NewID("evt")
	if err != nil {
		return err
	}
	correlationID, err := domain.NewID("cor")
	if err != nil {
		return err
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO events(event_id, type, target_id, payload, created_at, correlation_id)
VALUES(?, ?, ?, ?, ?, ?)`, eventID, eventType, targetID, payload, formatTime(now), correlationID)
	if err != nil {
		return fmt.Errorf("save %s event: %w", eventType, err)
	}
	return nil
}

func scanFolder(row scanner) (domain.Folder, error) {
	var folder domain.Folder
	var parent sql.NullString
	var status string
	var locked bool
	var createdAt, updatedAt string
	if err := row.Scan(&folder.ID, &parent, &folder.Name, &folder.CurrentPath, &folder.Kind,
		&folder.Purpose, &folder.Description, &status, &locked, &createdAt, &updatedAt); err != nil {
		return domain.Folder{}, err
	}
	folder.ParentID = parent.String
	folder.Status = domain.FolderStatus(status)
	folder.Locked = locked
	var err error
	if folder.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Folder{}, err
	}
	if folder.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.Folder{}, err
	}
	return folder, nil
}

func scanFile(row scanner) (domain.File, error) {
	var file domain.File
	var folderID sql.NullString
	var status string
	var modifiedAt, observedAt, createdAt, updatedAt string
	if err := row.Scan(&file.ID, &folderID, &file.Name, &file.CurrentPath, &file.SHA256, &file.Size,
		&file.MIME, &modifiedAt, &status, &file.ExtractStatus, &file.ExtractError,
		&observedAt, &createdAt, &updatedAt); err != nil {
		return domain.File{}, err
	}
	file.CurrentFolderID = folderID.String
	file.Status = domain.FileStatus(status)
	var err error
	if file.ModifiedAt, err = parseTime(modifiedAt); err != nil {
		return domain.File{}, err
	}
	if file.ObservedAt, err = parseTime(observedAt); err != nil {
		return domain.File{}, err
	}
	if file.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.File{}, err
	}
	if file.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.File{}, err
	}
	return file, nil
}

func scanProposal(row scanner) (domain.Proposal, error) {
	var proposal domain.Proposal
	var proposalType, state, createdAt, updatedAt string
	if err := row.Scan(&proposal.ID, &proposalType, &proposal.TargetID, &proposal.FromID,
		&proposal.ToID, &proposal.Score, &proposal.Reason, &state, &createdAt, &updatedAt); err != nil {
		return domain.Proposal{}, err
	}
	proposal.Type = domain.ProposalType(proposalType)
	proposal.State = domain.ProposalState(state)
	var err error
	if proposal.CreatedAt, err = parseTime(createdAt); err != nil {
		return domain.Proposal{}, err
	}
	if proposal.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return domain.Proposal{}, err
	}
	return proposal, nil
}

func filepathBase(path string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' || path[index] == '\\' {
			return path[index+1:]
		}
	}
	return path
}
