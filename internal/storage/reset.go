package storage

import (
	"context"
	"fmt"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

// ResetData removes indexed application data only. Source files and model weights are untouched.
func (db *DB) ResetData(ctx context.Context, documents, folders bool) (domain.ResetResult, error) {
	if !documents && !folders {
		return domain.ResetResult{}, fmt.Errorf("select documents or folders")
	}
	tx, err := db.sql.BeginTx(ctx, nil)
	if err != nil {
		return domain.ResetResult{}, err
	}
	defer tx.Rollback()
	var active int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ingest_jobs WHERE status IN ('queued','running')").Scan(&active); err != nil {
		return domain.ResetResult{}, err
	}
	if active != 0 {
		return domain.ResetResult{}, fmt.Errorf("wait for %d queued or running jobs before clearing data", active)
	}
	result := domain.ResetResult{}
	if documents || folders {
		if _, err := tx.ExecContext(ctx, "DELETE FROM decisions"); err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM proposals"); err != nil {
			return result, err
		}
	}
	if documents {
		for _, table := range []string{"model_runs", "file_search", "file_tags", "extracted_chunks", "file_versions"} {
			if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
				return result, err
			}
		}
		removed, err := tx.ExecContext(ctx, "DELETE FROM files")
		if err != nil {
			return result, err
		}
		result.Files, _ = removed.RowsAffected()
		removed, err = tx.ExecContext(ctx, "DELETE FROM ingest_jobs")
		if err != nil {
			return result, err
		}
		result.Jobs, _ = removed.RowsAffected()
		if _, err := tx.ExecContext(ctx, "DELETE FROM events"); err != nil {
			return result, err
		}
	} else if folders {
		if _, err := tx.ExecContext(ctx, "DELETE FROM model_runs"); err != nil {
			return result, err
		}
	}
	if folders {
		removed, err := tx.ExecContext(ctx, "DELETE FROM folders")
		if err != nil {
			return result, err
		}
		result.Folders, _ = removed.RowsAffected()
		if !documents {
			if _, err := tx.ExecContext(ctx, "UPDATE file_search SET folder_description = ''"); err != nil {
				return result, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.ResetResult{}, err
	}
	return result, nil
}
