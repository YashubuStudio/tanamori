package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

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
	return domain.MoveOperation{Proposal: proposal, File: file, Folder: folder}, nil
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
