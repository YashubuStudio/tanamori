package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

// ListRecentJobs returns newest jobs first for the local operator UI.
func (db *DB) ListRecentJobs(ctx context.Context, limit int) ([]jobqueue.Job, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := db.sql.QueryContext(ctx, "SELECT "+jobColumns+`
FROM ingest_jobs ORDER BY created_at DESC, job_id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent jobs: %w", err)
	}
	defer rows.Close()
	jobs := make([]jobqueue.Job, 0)
	for rows.Next() {
		job, scanErr := scanJob(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan recent job: %w", scanErr)
		}
		job.SourceKind = db.sourceKind(job.Path, job.SourceRelativePath)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent jobs: %w", err)
	}
	return jobs, nil
}

// ListClassifications returns recent files with folder scores but no file body.
func (db *DB) ListClassifications(ctx context.Context, limit int) ([]domain.ClassificationSummary, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := db.sql.QueryContext(ctx, `
WITH recent_files AS (
    SELECT file_id, current_folder_id, name, current_path, extract_status, extract_error, created_at
    FROM files ORDER BY created_at DESC, file_id DESC LIMIT ?
)
SELECT f.file_id, COALESCE(f.current_folder_id, ''), f.name, f.current_path,
       f.extract_status, f.extract_error, f.created_at,
       COALESCE((SELECT json_group_array(tag) FROM file_tags ft WHERE ft.file_id = f.file_id), '[]'),
       COALESCE(current_folder.name, ''),
       COALESCE(candidate.folder_id, ''), COALESCE(candidate.name, ''),
       COALESCE(CAST(json_extract(mr.scores, '$.score') AS REAL), 0),
       COALESCE(CAST(json_extract(mr.scores, '$.priority') AS INTEGER), 0),
       COALESCE(mr.model_version, ''), COALESCE(mr.status, ''),
       COALESCE(p.proposal_id, ''), COALESCE(p.state, '')
FROM recent_files f
LEFT JOIN folders current_folder ON current_folder.folder_id = f.current_folder_id
LEFT JOIN model_runs mr
  ON mr.task = 'classify_file_to_folder'
 AND json_extract(mr.input_refs, '$[0]') = f.file_id
 AND mr.run_id = (SELECT newer.run_id FROM model_runs newer
    WHERE newer.task = 'classify_file_to_folder'
      AND json_extract(newer.input_refs, '$[0]') = f.file_id
      AND json_extract(newer.input_refs, '$[1]') = json_extract(mr.input_refs, '$[1]')
    ORDER BY newer.created_at DESC, newer.run_id DESC LIMIT 1)
LEFT JOIN folders candidate ON candidate.folder_id = json_extract(mr.input_refs, '$[1]')
LEFT JOIN proposals p ON p.proposal_id = (
    SELECT p2.proposal_id FROM proposals p2
    WHERE p2.target_id = f.file_id ORDER BY p2.created_at DESC LIMIT 1
)
ORDER BY f.created_at DESC, f.file_id DESC,
         COALESCE(CAST(json_extract(mr.scores, '$.priority') AS INTEGER), 0) DESC,
         COALESCE(CAST(json_extract(mr.scores, '$.score') AS REAL), 0) DESC`, limit)
	if err != nil {
		return nil, fmt.Errorf("list classifications: %w", err)
	}
	defer rows.Close()

	result := make([]domain.ClassificationSummary, 0)
	indexes := make(map[string]int)
	for rows.Next() {
		var fileID, currentFolderID, name, path, extractStatus, extractError, created, tagsJSON string
		var currentFolder, candidateID, candidateName, modelVersion, runStatus string
		var proposalID, proposalState string
		var score float64
		var priority int
		if err := rows.Scan(&fileID, &currentFolderID, &name, &path, &extractStatus, &extractError,
			&created, &tagsJSON, &currentFolder, &candidateID, &candidateName, &score, &priority, &modelVersion,
			&runStatus, &proposalID, &proposalState); err != nil {
			return nil, fmt.Errorf("scan classification: %w", err)
		}
		index, ok := indexes[fileID]
		if !ok {
			var tags []string
			if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
				return nil, fmt.Errorf("decode classification tags: %w", err)
			}
			createdAt, parseErr := parseTime(created)
			if parseErr != nil {
				return nil, fmt.Errorf("parse classification time: %w", parseErr)
			}
			index = len(result)
			indexes[fileID] = index
			result = append(result, domain.ClassificationSummary{
				FileID: fileID, Name: name, Path: path, CurrentFolderID: currentFolderID,
				CurrentFolder: currentFolder, ExtractStatus: extractStatus, ExtractError: extractError,
				Candidates: make([]domain.ClassificationCandidate, 0), Tags: tags, ProposalID: proposalID,
				ProposalState: domain.ProposalState(proposalState), CreatedAt: createdAt,
			})
		}
		if candidateID != "" && runStatus == "ok" {
			result[index].Candidates = append(result[index].Candidates, domain.ClassificationCandidate{
				FolderID: candidateID, FolderName: candidateName, Score: score, Priority: priority, ModelVersion: modelVersion,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate classifications: %w", err)
	}
	for index := range result {
		if len(result[index].Candidates) > 0 {
			top := result[index].Candidates[0]
			result[index].Top = &top
		}
	}
	return result, nil
}
