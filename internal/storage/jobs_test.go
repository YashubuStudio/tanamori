package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

func TestJobRecoveryLeaseAndChangeDetection(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	path := filepath.Join(t.TempDir(), "one.txt")
	first, err := db.Put(ctx, jobqueue.KindFile, path, 10, base, true)
	if err != nil || first.Status != jobqueue.Queued {
		t.Fatalf("put: %+v %v", first, err)
	}
	if ok, err := db.AcquireLease(ctx, "owner-a"); err != nil || !ok {
		t.Fatalf("acquire: %v %v", ok, err)
	}
	if ok, err := db.AcquireLease(ctx, "owner-b"); err != nil || ok {
		t.Fatalf("second lease: %v %v", ok, err)
	}
	claimed, ok, err := db.Claim(ctx, jobqueue.KindFile)
	if err != nil || !ok || claimed.Attempts != 1 {
		t.Fatalf("claim: %+v %v %v", claimed, ok, err)
	}
	if err := db.RecoverRunning(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, err := db.Get(ctx, first.ID)
	if err != nil || recovered.Status != jobqueue.Queued {
		t.Fatalf("recover: %+v %v", recovered, err)
	}
	claimed, ok, err = db.Claim(ctx, jobqueue.KindFile)
	if err != nil || !ok || claimed.Attempts != 2 {
		t.Fatalf("reclaim: %+v %v %v", claimed, ok, err)
	}
	if err := db.Complete(ctx, claimed.ID, "fil_test"); err != nil {
		t.Fatal(err)
	}
	unchanged, err := db.Put(ctx, jobqueue.KindFile, path, 10, base, false)
	if err != nil || unchanged.Status != jobqueue.Succeeded {
		t.Fatalf("unchanged: %+v %v", unchanged, err)
	}
	changed, err := db.Put(ctx, jobqueue.KindFile, path, 11, base, false)
	if err != nil || changed.Status != jobqueue.Queued || changed.ID != first.ID {
		t.Fatalf("changed: %+v %v", changed, err)
	}
	if err := db.ReleaseLease(ctx, "owner-a"); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.AcquireLease(ctx, "owner-b"); err != nil || !ok {
		t.Fatalf("takeover: %v %v", ok, err)
	}
	for attempt := 1; attempt <= jobqueue.MaxAttempts; attempt++ {
		current, ok, err := db.Claim(ctx, jobqueue.KindFile)
		if err != nil || !ok || current.Attempts != attempt {
			t.Fatalf("claim %d: %+v %v %v", attempt, current, ok, err)
		}
		if err := db.RecoverRunning(ctx); err != nil {
			t.Fatal(err)
		}
	}
	exhausted, err := db.Get(ctx, first.ID)
	if err != nil || exhausted.Status != jobqueue.Failed {
		t.Fatalf("interrupted job exceeded retry limit: %+v %v", exhausted, err)
	}
}
