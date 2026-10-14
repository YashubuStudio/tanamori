package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

func TestWorkLogPersistsQueueTransitionsAndBrowserOrigin(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := Open(ctx, filepath.Join(root, "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "incoming", "session", "folder", "note.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUploadOrigin(ctx, path, "folder/note.txt"); err != nil {
		t.Fatal(err)
	}
	job, err := db.Put(ctx, jobqueue.KindFile, path, 4, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.Claim(ctx, jobqueue.KindFile); err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if err := db.Fail(ctx, job.ID, "temporary failure", true); err != nil {
		t.Fatal(err)
	}
	logs, err := db.ListWorkLogs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, entry := range logs {
		seen[entry.EventType] = true
		if entry.JobID != job.ID || entry.SourceKind != domain.SourceBrowserUpload || entry.SourceRelativePath != "folder/note.txt" {
			t.Fatalf("incorrect work log: %+v", entry)
		}
	}
	if len(logs) != 3 || !seen["JOB_QUEUED"] || !seen["JOB_STARTED"] || !seen["JOB_RETRY"] {
		t.Fatalf("missing persisted transitions: %+v", logs)
	}
	if _, err := db.sql.ExecContext(ctx, "UPDATE ingest_jobs SET available_at = ? WHERE job_id = ?", formatTime(time.Now().UTC().Add(-time.Second)), job.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.Claim(ctx, jobqueue.KindFile); err != nil || !ok {
		t.Fatalf("retry claim: ok=%v err=%v", ok, err)
	}
	if err := db.Complete(ctx, job.ID, "fil_test"); err != nil {
		t.Fatal(err)
	}
	logs, err = db.ListWorkLogs(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 5 {
		t.Fatalf("want five transition records, got %+v", logs)
	}
	seen = map[string]bool{}
	for _, entry := range logs {
		seen[entry.EventType] = true
	}
	if !seen["JOB_COMPLETED"] {
		t.Fatalf("completion was not logged: %+v", logs)
	}
}

func TestJobOriginSnapshotSurvivesRemovalAndPathReuse(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(db.Path()), "incoming", "session", "note.txt")
	if err := db.RecordUploadOrigin(ctx, path, "original/note.txt"); err != nil {
		t.Fatal(err)
	}
	job, err := db.Put(ctx, jobqueue.KindFile, path, 4, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.Claim(ctx, jobqueue.KindFile); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	// A move can remove the live origin before the worker records completion.
	if err := db.DeleteUploadOrigin(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUploadOrigin(ctx, path, "replacement/note.txt"); err != nil {
		t.Fatal(err)
	}
	if err := db.Complete(ctx, job.ID, "fil_test"); err != nil {
		t.Fatal(err)
	}
	jobs, err := db.ListRecentJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].SourceRelativePath != "original/note.txt" {
		t.Fatalf("job origin changed after reuse: %+v %v", jobs, err)
	}
	logs, err := db.ListWorkLogs(ctx, 100)
	if err != nil || len(logs) != 3 {
		t.Fatalf("logs: %+v %v", logs, err)
	}
	for _, entry := range logs {
		if entry.SourceRelativePath != "original/note.txt" {
			t.Fatalf("historical origin changed: %+v", entry)
		}
	}
	if _, err := db.Put(ctx, jobqueue.KindFile, path, 8, time.Now(), true); err != nil {
		t.Fatal(err)
	}
	jobs, err = db.ListRecentJobs(ctx, 10)
	if err != nil || len(jobs) != 1 || jobs[0].SourceRelativePath != "replacement/note.txt" {
		t.Fatalf("requeued job did not snapshot new origin: %+v %v", jobs, err)
	}
	logs, err = db.ListWorkLogs(ctx, 100)
	if err != nil || len(logs) != 4 {
		t.Fatalf("logs after requeue: %+v %v", logs, err)
	}
	for _, entry := range logs {
		want := "original/note.txt"
		if entry.EventType == "JOB_QUEUED" && entry.At.After(job.CreatedAt) {
			want = "replacement/note.txt"
		}
		if entry.SourceRelativePath != want {
			t.Fatalf("wrong snapshot: %+v want %s", entry, want)
		}
	}
}

func TestMigrateLegacyJobOriginFromPersistedLog(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(filepath.Dir(db.Path()), "incoming", "note.txt")
	if err := db.RecordUploadOrigin(ctx, path, "original/note.txt"); err != nil {
		t.Fatal(err)
	}
	job, err := db.Put(ctx, jobqueue.KindFile, path, 4, time.Now(), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUploadOrigin(ctx, path); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordUploadOrigin(ctx, path, "replacement/note.txt"); err != nil {
		t.Fatal(err)
	}
	// Reconstruct the pre-snapshot schema while retaining its persisted logs.
	if _, err := db.sql.ExecContext(ctx, `DROP TRIGGER trg_ingest_job_queued_log;
DROP TRIGGER trg_ingest_job_state_log;
ALTER TABLE ingest_jobs DROP COLUMN source_relative_path;
DELETE FROM schema_migrations WHERE version = 4;`); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := db.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		got, err := db.Get(ctx, job.ID)
		if err != nil || got.SourceRelativePath != "original/note.txt" {
			t.Fatalf("migration %d: %+v %v", index, got, err)
		}
	}
}
