package storage

import (
	"context"
	"fmt"
	"os"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

func (db *DB) PreviewReset(ctx context.Context) (domain.ResetPreview, error) {
	var preview domain.ResetPreview
	err := db.sql.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM files),
		(SELECT COUNT(*) FROM folders),
		(SELECT COUNT(*) FROM ingest_jobs),
		(SELECT COUNT(*) FROM ingest_jobs WHERE status IN ('queued','running'))`).Scan(
		&preview.Files, &preview.Folders, &preview.Jobs, &preview.ActiveJobs)
	if err != nil {
		return domain.ResetPreview{}, err
	}
	return preview, nil
}

// ResetData removes indexed application data only. Source files and model weights are untouched.
func (db *DB) ResetData(ctx context.Context, documents, folders bool) (result domain.ResetResult, err error) {
	if !documents && !folders {
		return domain.ResetResult{}, fmt.Errorf("select documents or folders")
	}
	preview, err := db.PreviewReset(ctx)
	if err != nil {
		return domain.ResetResult{}, err
	}
	if preview.ActiveJobs > 0 {
		return domain.ResetResult{}, fmt.Errorf("wait for %d queued or running jobs before clearing data", preview.ActiveJobs)
	}
	backupPath, err := db.Backup(ctx)
	if err != nil {
		return domain.ResetResult{}, err
	}
	// From here on, any error must not leave an unused backup behind: a
	// backup is only meaningful alongside the delete it was made for.
	defer func() {
		if err != nil {
			_ = os.Remove(backupPath)
		}
	}()
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
	result = domain.ResetResult{BackupPath: backupPath}
	if documents || folders {
		if _, err := tx.ExecContext(ctx, "DELETE FROM decisions"); err != nil {
			return result, err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM proposals"); err != nil {
			return result, err
		}
	}
	if documents {
		for _, table := range []string{"model_runs", "file_search", "file_tags", "extracted_chunks", "file_versions", "upload_origins"} {
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
