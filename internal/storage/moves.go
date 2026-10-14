package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

// CreateManualProposal lets a human pick a destination for a file the model
// left unclassified or misclassified (e.g. an abstained candidate shown in
// the UI). It only records a pending proposal; the existing apply/reject
// flow still requires an explicit human confirmation before any file moves.
func (db *DB) CreateManualProposal(ctx context.Context, fileID, folderID string, score float64, reason string, now time.Time) (domain.Proposal, error) {
	transaction, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("begin manual proposal: %w", err)
	}
	defer transaction.Rollback()
	file, err := scanFile(transaction.QueryRowContext(ctx, `
SELECT file_id, current_folder_id, name, current_path, sha256, size, mime, modified_at, status,
       extract_status, extract_error, observed_at, created_at, updated_at
FROM files WHERE file_id = ?`, fileID))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("load file: %w", err)
	}
	folder, err := scanFolder(transaction.QueryRowContext(ctx, `
SELECT folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at
FROM folders WHERE folder_id = ?`, folderID))
	if err != nil {
		return domain.Proposal{}, fmt.Errorf("load folder: %w", err)
	}
	if folder.Status != domain.FolderManaged || folder.Locked {
		return domain.Proposal{}, fmt.Errorf("destination folder is not writable by policy")
	}
	if folder.ID == file.CurrentFolderID {
		return domain.Proposal{}, fmt.Errorf("file is already in this folder")
	}
	var pending int
	if err := transaction.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM proposals WHERE target_id = ? AND state = 'pending'", file.ID).Scan(&pending); err != nil {
		return domain.Proposal{}, fmt.Errorf("check pending proposals: %w", err)
	}
	if pending > 0 {
		return domain.Proposal{}, fmt.Errorf("a pending proposal already exists for this file")
	}
	proposalID, err := domain.NewID("prp")
	if err != nil {
		return domain.Proposal{}, err
	}
	proposal := domain.Proposal{
		ID: proposalID, Type: domain.ProposalMoveFile, TargetID: file.ID, FromID: file.CurrentFolderID,
		ToID: folder.ID, Score: score, Reason: reason, State: domain.ProposalPending, CreatedAt: now, UpdatedAt: now,
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO proposals(proposal_id, type, target_id, from_id, to_id, score, reason, state, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, proposal.ID, proposal.Type, proposal.TargetID, proposal.FromID,
		proposal.ToID, proposal.Score, proposal.Reason, proposal.State, formatTime(now), formatTime(now)); err != nil {
		return domain.Proposal{}, fmt.Errorf("save manual proposal: %w", err)
	}
	payload, _ := json.Marshal(map[string]string{"folder_id": folder.ID, "reason": reason})
	if err := insertEvent(ctx, transaction, "PROPOSAL_CREATED_MANUALLY", file.ID, string(payload), now); err != nil {
		return domain.Proposal{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domain.Proposal{}, fmt.Errorf("commit manual proposal: %w", err)
	}
	proposal.FileName, proposal.FilePath, proposal.FolderName, proposal.FolderPath = file.Name, file.CurrentPath, folder.Name, folder.CurrentPath
	proposal.DestinationDirectory = folder.CurrentPath
	return proposal, nil
}

func (db *DB) GetMoveOperation(ctx context.Context, proposalID string) (domain.MoveOperation, error) {
	proposal, err := scanProposal(db.sql.QueryRowContext(ctx, `
SELECT proposal_id, type, target_id, from_id, to_id, score, reason, state, created_at, updated_at
FROM proposals WHERE proposal_id = ?`, proposalID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load proposal: %w", err)
	}
	if proposal.State != domain.ProposalPending {
		return domain.MoveOperation{}, fmt.Errorf("proposal is not pending")
	}
	file, err := scanFile(db.sql.QueryRowContext(ctx, `
SELECT file_id, current_folder_id, name, current_path, sha256, size, mime, modified_at, status,
extract_status, extract_error, observed_at, created_at, updated_at FROM files WHERE file_id = ?`, proposal.TargetID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load proposal file: %w", err)
	}
	folder, err := scanFolder(db.sql.QueryRowContext(ctx, `
SELECT folder_id, parent_id, name, current_path, kind, purpose, description, status, locked, created_at, updated_at
FROM folders WHERE folder_id = ?`, proposal.ToID))
	if err != nil {
		return domain.MoveOperation{}, fmt.Errorf("load destination folder: %w", err)
	}
	relative, err := db.uploadRelativePath(ctx, file.CurrentPath)
	if err != nil {
		return domain.MoveOperation{}, err
	}
	return domain.MoveOperation{Proposal: proposal, File: file, Folder: folder,
		SourceKind: db.sourceKind(file.CurrentPath, relative), SourceRelativePath: relative}, nil
}

func (db *DB) RejectMove(ctx context.Context, proposalID string, actor domain.DecisionActor, reason string, now time.Time) error {
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE proposals SET state=?, updated_at=? WHERE proposal_id=? AND state=?",
		domain.ProposalRejected, formatTime(now), proposalID, domain.ProposalPending)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("proposal is not pending")
	}
	decisionID, err := domain.NewID("dec")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO decisions(decision_id, proposal_id, actor, action, reason, decided_at)
VALUES(?, ?, ?, 'reject', ?, ?)`, decisionID, proposalID, actor, reason, formatTime(now)); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, "MOVE_REJECTED", proposalID, `{"reason":"user skipped"}`, now); err != nil {
		return err
	}
	return tx.Commit()
}
