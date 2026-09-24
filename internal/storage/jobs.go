package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

const jobColumns = "job_id, path, kind, status, attempts, available_at, created_at, updated_at, started_at, finished_at, file_id, error"

func scanJob(row scanner) (jobqueue.Job, error) {
	var job jobqueue.Job
	var kind, status, available, created, updated string
	var started, finished sql.NullString
	err := row.Scan(&job.ID, &job.Path, &kind, &status, &job.Attempts, &available, &created, &updated,
		&started, &finished, &job.FileID, &job.Error)
	if err != nil {
		return jobqueue.Job{}, err
	}
	job.Kind, job.Status = jobqueue.Kind(kind), jobqueue.Status(status)
	if job.AvailableAt, err = parseTime(available); err != nil {
		return jobqueue.Job{}, err
	}
	if job.CreatedAt, err = parseTime(created); err != nil {
		return jobqueue.Job{}, err
	}
	if job.UpdatedAt, err = parseTime(updated); err != nil {
		return jobqueue.Job{}, err
	}
	if started.Valid {
		value, parseErr := parseTime(started.String)
		if parseErr != nil {
			return jobqueue.Job{}, parseErr
		}
		job.StartedAt = &value
	}
	if finished.Valid {
		value, parseErr := parseTime(finished.String)
		if parseErr != nil {
			return jobqueue.Job{}, parseErr
		}
		job.FinishedAt = &value
	}
	return job, nil
}

func (db *DB) Put(ctx context.Context, kind jobqueue.Kind, path string, size int64, modified time.Time, force bool) (jobqueue.Job, error) {
	if kind != jobqueue.KindFile && kind != jobqueue.KindScan {
		return jobqueue.Job{}, fmt.Errorf("invalid job kind %q", kind)
	}
	id, err := domain.NewID("job")
	if err != nil {
		return jobqueue.Job{}, err
	}
	now := formatTime(time.Now().UTC())
	modtime := formatTime(modified)
	query := `INSERT INTO ingest_jobs(job_id, path, kind, status, attempts, available_at, created_at, updated_at, file_id, error, source_size, source_mtime)
VALUES(?, ?, ?, 'queued', 0, ?, ?, ?, '', '', ?, ?)
ON CONFLICT(kind, path) DO UPDATE SET
  status='queued', attempts=0, available_at=excluded.available_at, updated_at=excluded.updated_at,
  started_at=NULL, finished_at=NULL, file_id='', error='', source_size=excluded.source_size,
  source_mtime=excluded.source_mtime
WHERE ingest_jobs.status != 'running' AND
  (? OR ingest_jobs.source_size != excluded.source_size OR ingest_jobs.source_mtime != excluded.source_mtime)
RETURNING ` + jobColumns
	job, err := scanJob(db.sql.QueryRowContext(ctx, query, id, path, kind, now, now, now, size, modtime, force))
	if errors.Is(err, sql.ErrNoRows) {
		return db.GetByPath(ctx, kind, path)
	}
	if err != nil {
		return jobqueue.Job{}, fmt.Errorf("enqueue %s: %w", path, err)
	}
	return job, nil
}

func (db *DB) GetByPath(ctx context.Context, kind jobqueue.Kind, path string) (jobqueue.Job, error) {
	job, err := scanJob(db.sql.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM ingest_jobs WHERE kind=? AND path=?", kind, path))
	if err != nil {
		return jobqueue.Job{}, fmt.Errorf("get queued path: %w", err)
	}
	return job, nil
}

func (db *DB) Get(ctx context.Context, id string) (jobqueue.Job, error) {
	job, err := scanJob(db.sql.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM ingest_jobs WHERE job_id=?", id))
	if err != nil {
		return jobqueue.Job{}, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (db *DB) Claim(ctx context.Context, kind jobqueue.Kind) (jobqueue.Job, bool, error) {
	now := formatTime(time.Now().UTC())
	query := `UPDATE ingest_jobs SET status='running', attempts=attempts+1, started_at=?, updated_at=?
WHERE job_id=(SELECT job_id FROM ingest_jobs WHERE kind=? AND status='queued' AND available_at<=?
ORDER BY created_at, job_id LIMIT 1) AND status='queued'
RETURNING ` + jobColumns
	job, err := scanJob(db.sql.QueryRowContext(ctx, query, now, now, kind, now))
	if errors.Is(err, sql.ErrNoRows) {
		return jobqueue.Job{}, false, nil
	}
	if err != nil {
		return jobqueue.Job{}, false, fmt.Errorf("claim job: %w", err)
	}
	return job, true, nil
}

func (db *DB) Complete(ctx context.Context, id, fileID string) error {
	result, err := db.sql.ExecContext(ctx, `UPDATE ingest_jobs SET status='succeeded', file_id=?, error='',
finished_at=?, updated_at=? WHERE job_id=? AND status='running'`, fileID, formatTime(time.Now().UTC()), formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("job %s is not running", id)
	}
	return nil
}

func (db *DB) Fail(ctx context.Context, id, message string, retry bool) error {
	job, err := db.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status != jobqueue.Running {
		return fmt.Errorf("job %s is not running", id)
	}
	message = strings.TrimSpace(message)
	if len(message) > 2048 {
		message = message[:2048]
	}
	status := jobqueue.Failed
	available := time.Now().UTC()
	if retry {
		status = jobqueue.Queued
		available = available.Add(time.Duration(1<<min(job.Attempts, 8)) * time.Second)
	}
	now := formatTime(time.Now().UTC())
	result, err := db.sql.ExecContext(ctx, `UPDATE ingest_jobs SET status=?, error=?, available_at=?,
updated_at=?, finished_at=? WHERE job_id=? AND status='running'`,
		status, message, formatTime(available), now, nullableFinish(retry, now), id)
	if err != nil {
		return fmt.Errorf("fail job: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return fmt.Errorf("job %s changed during failure recording", id)
	}
	return nil
}

func nullableFinish(retry bool, now string) any {
	if retry {
		return nil
	}
	return now
}

func (db *DB) Counts(ctx context.Context) (jobqueue.Counts, error) {
	var counts jobqueue.Counts
	err := db.sql.QueryRowContext(ctx, `SELECT
COUNT(*) FILTER (WHERE status='queued'),
COUNT(*) FILTER (WHERE status='running'),
COUNT(*) FILTER (WHERE status='succeeded'),
COUNT(*) FILTER (WHERE status='failed') FROM ingest_jobs`).Scan(
		&counts.Queued, &counts.Running, &counts.Succeeded, &counts.Failed)
	if err != nil {
		return jobqueue.Counts{}, fmt.Errorf("count jobs: %w", err)
	}
	return counts, nil
}

func (db *DB) OutstandingFiles(ctx context.Context) (int64, error) {
	var count int64
	err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM ingest_jobs
WHERE kind='file' AND status IN ('queued', 'running')`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count outstanding files: %w", err)
	}
	return count, nil
}

const leaseName = "ingest-worker"
const leaseDuration = 15 * time.Second

func (db *DB) AcquireLease(ctx context.Context, owner string) (bool, error) {
	now := time.Now().UTC()
	query := `INSERT INTO queue_lease(name, owner, expires_at) VALUES(?, ?, ?)
ON CONFLICT(name) DO UPDATE SET owner=excluded.owner, expires_at=excluded.expires_at
WHERE queue_lease.expires_at < ? OR queue_lease.owner=excluded.owner
RETURNING owner`
	var claimed string
	err := db.sql.QueryRowContext(ctx, query, leaseName, owner, formatTime(now.Add(leaseDuration)), formatTime(now)).Scan(&claimed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("acquire worker lease: %w", err)
	}
	return claimed == owner, nil
}

func (db *DB) RenewLease(ctx context.Context, owner string) (bool, error) {
	now := time.Now().UTC()
	result, err := db.sql.ExecContext(ctx, `UPDATE queue_lease SET expires_at=?
WHERE name=? AND owner=? AND expires_at>=?`, formatTime(now.Add(leaseDuration)), leaseName, owner, formatTime(now))
	if err != nil {
		return false, fmt.Errorf("renew worker lease: %w", err)
	}
	affected, _ := result.RowsAffected()
	return affected == 1, nil
}

func (db *DB) ReleaseLease(ctx context.Context, owner string) error {
	_, err := db.sql.ExecContext(ctx, "DELETE FROM queue_lease WHERE name=? AND owner=?", leaseName, owner)
	if err != nil {
		return fmt.Errorf("release worker lease: %w", err)
	}
	return nil
}

func (db *DB) RecoverRunning(ctx context.Context) error {
	now := formatTime(time.Now().UTC())
	_, err := db.sql.ExecContext(ctx, `UPDATE ingest_jobs SET
status=CASE WHEN attempts>=? THEN 'failed' ELSE 'queued' END,
error=CASE WHEN attempts>=? THEN 'worker interrupted after maximum attempts' ELSE error END,
finished_at=CASE WHEN attempts>=? THEN ? ELSE NULL END,
available_at=?, updated_at=?, started_at=NULL WHERE status='running'`,
		jobqueue.MaxAttempts, jobqueue.MaxAttempts, jobqueue.MaxAttempts, now, now, now)
	if err != nil {
		return fmt.Errorf("recover interrupted jobs: %w", err)
	}
	return nil
}
