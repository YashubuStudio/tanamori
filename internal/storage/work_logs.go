package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

// ListWorkLogs returns bounded, persisted queue transitions newest first.
func (db *DB) ListWorkLogs(ctx context.Context, limit int) ([]jobqueue.WorkLog, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT created_at, type, target_id, payload FROM events
WHERE type IN ('JOB_QUEUED', 'JOB_STARTED', 'JOB_RETRY', 'JOB_COMPLETED', 'JOB_FAILED')
ORDER BY created_at DESC, event_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list work logs: %w", err)
	}
	defer rows.Close()
	result := make([]jobqueue.WorkLog, 0)
	for rows.Next() {
		var entry jobqueue.WorkLog
		var timestamp, payload string
		if err := rows.Scan(&timestamp, &entry.EventType, &entry.JobID, &payload); err != nil {
			return nil, fmt.Errorf("scan work log: %w", err)
		}
		if entry.At, err = parseTime(timestamp); err != nil {
			return nil, fmt.Errorf("parse work log time: %w", err)
		}
		var details struct {
			Path         string        `json:"path"`
			Kind         jobqueue.Kind `json:"kind"`
			Attempts     int           `json:"attempts"`
			Error        string        `json:"error"`
			RelativePath string        `json:"relative_path"`
		}
		if err := json.Unmarshal([]byte(payload), &details); err != nil {
			entry.Error = "ログの内容を解析できません: " + err.Error()
		} else {
			entry.Path, entry.Kind, entry.Attempts, entry.Error = details.Path, details.Kind, details.Attempts, details.Error
			entry.SourceRelativePath = details.RelativePath
		}
		entry.SourceKind = db.sourceKind(entry.Path, entry.SourceRelativePath)
		result = append(result, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate work logs: %w", err)
	}
	return result, nil
}
