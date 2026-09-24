package jobqueue_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/jobqueue"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

func TestCapacity(t *testing.T) {
	for _, test := range []struct {
		cpu             int
		memory          uint64
		requested, want int
	}{
		{4, 64 << 30, 0, 1}, {8, 8 << 30, 0, 1}, {8, 16 << 30, 0, 2},
		{16, 64 << 30, 1, 1}, {16, 64 << 30, 20, 2},
	} {
		if got := jobqueue.Capacity(test.cpu, test.memory, test.requested); got != test.want {
			t.Fatalf("Capacity(%d,%d,%d)=%d, want %d", test.cpu, test.memory, test.requested, got, test.want)
		}
	}
}

func TestDirectoryScanHasBoundedBacklogAndOneWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	for index := 0; index < 30; index++ {
		path := filepath.Join(root, fmt.Sprintf("file-%03d.txt", index))
		if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := jobqueue.Enqueue(ctx, db, root)
	if err != nil || job.Kind != jobqueue.KindScan {
		t.Fatalf("enqueue: %+v %v", job, err)
	}
	var active, maximum, processed atomic.Int64
	runner := jobqueue.Runner{Store: db, Workers: 1, QueueLimit: 3,
		Ingest: func(ctx context.Context, path string) (string, error) {
			current := active.Add(1)
			if current > maximum.Load() {
				maximum.Store(current)
			}
			defer active.Add(-1)
			time.Sleep(15 * time.Millisecond)
			processed.Add(1)
			return filepath.Base(path), nil
		}}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	deadline := time.After(15 * time.Second)
	for {
		counts, err := db.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		outstanding, err := db.OutstandingFiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if outstanding > 3 {
			t.Fatalf("backlog exceeded limit: %d", outstanding)
		}
		if counts.Succeeded == 31 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out: counts=%+v processed=%d", counts, processed.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if processed.Load() != 30 || maximum.Load() != 1 {
		t.Fatalf("processed=%d maximum active=%d", processed.Load(), maximum.Load())
	}
}

func TestThousandFilesStopAtBacklogLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	for index := 0; index < 1000; index++ {
		path := filepath.Join(root, fmt.Sprintf("item-%04d.txt", index))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "thousand.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := jobqueue.Enqueue(ctx, db, root); err != nil {
		t.Fatal(err)
	}
	var active atomic.Int64
	runner := jobqueue.Runner{Store: db, Workers: 2, QueueLimit: 7,
		Ingest: func(ctx context.Context, path string) (string, error) {
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			return "", ctx.Err()
		}}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	deadline := time.After(10 * time.Second)
	for {
		count, err := db.OutstandingFiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if count > 7 {
			t.Fatalf("backlog grew beyond seven: %d", count)
		}
		if count == 7 && active.Load() == 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("queue never reached limit: count=%d active=%d", count, active.Load())
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(200 * time.Millisecond)
	count, err := db.OutstandingFiles(ctx)
	if err != nil || count != 7 {
		t.Fatalf("backpressure did not hold: %d %v", count, err)
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
