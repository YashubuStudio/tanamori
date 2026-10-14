package jobqueue_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

type stopOnScanClaim struct {
	*storage.DB
	stop chan struct{}
}

func (store stopOnScanClaim) Claim(ctx context.Context, kind jobqueue.Kind) (jobqueue.Job, bool, error) {
	job, ok, err := store.DB.Claim(ctx, kind)
	if err == nil && ok && kind == jobqueue.KindScan {
		close(store.stop)
	}
	return job, ok, err
}

func TestRepeatedGracefulScanStopDoesNotExhaustAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	root := t.TempDir()
	input := filepath.Join(root, "input")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(input, "note.txt"), []byte("text"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := jobqueue.Enqueue(ctx, db, input)
	if err != nil {
		t.Fatal(err)
	}
	runner := jobqueue.Runner{Workers: 1, QueueLimit: 2,
		Ingest: func(context.Context, string) (string, error) { return "file-id", nil }}
	for index := 0; index < jobqueue.MaxAttempts+1; index++ {
		stop := make(chan struct{})
		runner.Store = stopOnScanClaim{DB: db, stop: stop}
		if err := runner.RunUntil(ctx, stop); err != nil {
			t.Fatal(err)
		}
		got, err := db.Get(ctx, job.ID)
		if err != nil || got.Status != jobqueue.Queued || got.Attempts != 0 || got.StartedAt != nil {
			t.Fatalf("shutdown %d: job=%+v err=%v", index, got, err)
		}
	}
	runner.Store = db
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- runner.RunUntil(ctx, stop) }()
	for {
		counts, err := db.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if counts.Succeeded == 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("scan did not resume: %+v", counts)
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(stop)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestGracefulStopFinishesClaimedFileAndLeavesNextQueued(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := t.TempDir()
	db, err := storage.Open(ctx, filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	firstPath := filepath.Join(root, "first.txt")
	secondPath := filepath.Join(root, "second.txt")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := jobqueue.Enqueue(ctx, db, firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := jobqueue.Enqueue(ctx, db, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	stop := make(chan struct{})
	runner := jobqueue.Runner{Store: db, Workers: 1, QueueLimit: 2,
		Ingest: func(ctx context.Context, path string) (string, error) {
			close(started)
			select {
			case <-release:
				return "file-id", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}}
	done := make(chan error, 1)
	go func() { done <- runner.RunUntil(ctx, stop) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(stop)
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	firstJob, err := db.Get(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondJob, err := db.Get(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if firstJob.Status != jobqueue.Succeeded || secondJob.Status != jobqueue.Queued {
		t.Fatalf("unexpected drain states: first=%s second=%s", firstJob.Status, secondJob.Status)
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

// TANAMORI_STRESS=1 go test -run TestTwentyThousandFiles -v ./internal/jobqueue
func TestTwentyThousandFiles(t *testing.T) {
	if os.Getenv("TANAMORI_STRESS") != "1" {
		t.Skip("set TANAMORI_STRESS=1 for the large scan")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root := t.TempDir()
	input := filepath.Join(root, "input")
	if err := os.Mkdir(input, 0o700); err != nil {
		t.Fatal(err)
	}
	const files = 20_000
	for index := 0; index < files; index++ {
		path := filepath.Join(input, fmt.Sprintf("item-%05d.txt", index))
		if err := os.WriteFile(path, []byte("text"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	db, err := storage.Open(ctx, filepath.Join(root, "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := jobqueue.Enqueue(ctx, db, input); err != nil {
		t.Fatal(err)
	}
	runner := jobqueue.Runner{Store: db, Workers: 2, QueueLimit: 64,
		Ingest: func(ctx context.Context, path string) (string, error) { return filepath.Base(path), nil }}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- runner.Run(runCtx) }()
	var peakAlloc uint64
	var peakOutstanding int64
	for {
		counts, err := db.Counts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		outstanding, err := db.OutstandingFiles(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if outstanding > peakOutstanding {
			peakOutstanding = outstanding
		}
		if outstanding > 64 {
			t.Fatalf("backlog exceeded 64: %d", outstanding)
		}
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		if memory.Alloc > peakAlloc {
			peakAlloc = memory.Alloc
		}
		if counts.Succeeded == files+1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	t.Logf("files=%d elapsed=%s peak outstanding=%d peak Go heap=%d MiB baseline=%d MiB",
		files, time.Since(started), peakOutstanding, peakAlloc>>20, baseline.Alloc>>20)
}
