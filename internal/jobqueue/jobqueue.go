// Package jobqueue processes file paths from a durable, bounded queue.
package jobqueue

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Kind string
type Status string

const (
	KindFile    Kind   = "file"
	KindScan    Kind   = "scan"
	Queued      Status = "queued"
	Running     Status = "running"
	Succeeded   Status = "succeeded"
	Failed      Status = "failed"
	MaxAttempts        = 3
)

type Job struct {
	ID          string     `json:"job_id"`
	Path        string     `json:"path"`
	Kind        Kind       `json:"kind"`
	Status      Status     `json:"status"`
	Attempts    int        `json:"attempts"`
	AvailableAt time.Time  `json:"available_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	FileID      string     `json:"file_id,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type Counts struct {
	Queued    int64 `json:"queued"`
	Running   int64 `json:"running"`
	Succeeded int64 `json:"succeeded"`
	Failed    int64 `json:"failed"`
}

type Store interface {
	Put(context.Context, Kind, string, int64, time.Time, bool) (Job, error)
	Claim(context.Context, Kind) (Job, bool, error)
	Complete(context.Context, string, string) error
	Fail(context.Context, string, string, bool) error
	Get(context.Context, string) (Job, error)
	Counts(context.Context) (Counts, error)
	OutstandingFiles(context.Context) (int64, error)
	AcquireLease(context.Context, string) (bool, error)
	RenewLease(context.Context, string) (bool, error)
	ReleaseLease(context.Context, string) error
	RecoverRunning(context.Context) error
}

var ErrQueueFull = errors.New("file queue is full; retry later or enqueue a directory")

func Enqueue(ctx context.Context, store Store, path string) (Job, error) {
	return EnqueueLimited(ctx, store, path, 0)
}

func EnqueueLimited(ctx context.Context, store Store, path string, outstandingLimit int64) (Job, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return Job{}, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return Job{}, fmt.Errorf("inspect path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Job{}, fmt.Errorf("symbolic links are not accepted: %s", absolute)
	}
	kind := KindFile
	if info.IsDir() {
		kind = KindScan
	} else if !info.Mode().IsRegular() {
		return Job{}, fmt.Errorf("not a regular file or directory: %s", absolute)
	}
	if kind == KindFile && outstandingLimit > 0 {
		count, err := store.OutstandingFiles(ctx)
		if err != nil {
			return Job{}, err
		}
		if count >= outstandingLimit {
			return Job{}, ErrQueueFull
		}
	}
	return store.Put(ctx, kind, filepath.Clean(absolute), info.Size(), info.ModTime().UTC(), true)
}

// Capacity caps file workers at two because the BGE process itself is large.
func Capacity(cpu int, availableBytes uint64, requested int) int {
	if cpu < 1 {
		cpu = runtime.NumCPU()
	}
	workers := 1
	if cpu >= 8 && availableBytes >= 16<<30 {
		workers = 2
	}
	if requested > 0 && requested < workers {
		workers = requested
	}
	return workers
}

type Runner struct {
	Store      Store
	Ingest     func(context.Context, string) (string, error)
	Workers    int
	QueueLimit int64
	Log        func(string, ...any)
}

func (r Runner) log(format string, args ...any) {
	if r.Log != nil {
		r.Log(format, args...)
	}
}

func (r Runner) Run(ctx context.Context) error {
	if r.Store == nil || r.Ingest == nil {
		return errors.New("queue store and ingest function are required")
	}
	if r.Workers < 1 || r.Workers > 2 {
		return fmt.Errorf("workers must be between 1 and 2")
	}
	if r.QueueLimit < 1 {
		return fmt.Errorf("queue limit must be positive")
	}
	owner := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	acquired, err := r.Store.AcquireLease(ctx, owner)
	if err != nil {
		return err
	}
	if !acquired {
		return errors.New("another TANAMORI queue worker is active")
	}
	defer r.Store.ReleaseLease(context.Background(), owner)
	if err := r.Store.RecoverRunning(ctx); err != nil {
		return err
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-ticker.C:
				ok, err := r.Store.RenewLease(workCtx, owner)
				if err != nil || !ok {
					r.log("queue lease lost: %v", err)
					cancel()
					return
				}
			}
		}
	}()
	wg.Add(1)
	go func() { defer wg.Done(); r.loop(workCtx, KindScan) }()
	for index := 0; index < r.Workers; index++ {
		wg.Add(1)
		go func() { defer wg.Done(); r.loop(workCtx, KindFile) }()
	}
	<-workCtx.Done()
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	return workCtx.Err()
}

func (r Runner) loop(ctx context.Context, kind Kind) {
	memoryPaused := false
	for ctx.Err() == nil {
		if kind == KindFile {
			available := AvailableMemory()
			if available > 0 && available < 1<<30 {
				if !memoryPaused {
					r.log("free memory below 1 GiB; pausing new file jobs")
				}
				memoryPaused = true
				pause(ctx, time.Second)
				continue
			}
			if memoryPaused {
				r.log("free memory recovered; resuming file jobs")
				memoryPaused = false
			}
		}
		job, ok, err := r.Store.Claim(ctx, kind)
		if err != nil {
			r.log("claim %s job: %v", kind, err)
			pause(ctx, time.Second)
			continue
		}
		if !ok {
			pause(ctx, 250*time.Millisecond)
			continue
		}
		var fileID string
		if kind == KindScan {
			err = r.scan(ctx, job.Path)
		} else {
			fileID, err = r.Ingest(ctx, job.Path)
		}
		if ctx.Err() != nil {
			return
		} // recovered on the next start
		if err != nil {
			retry := job.Attempts < MaxAttempts
			r.log("%s job %s failed (attempt %d): %v", kind, job.ID, job.Attempts, err)
			if updateErr := r.Store.Fail(ctx, job.ID, err.Error(), retry); updateErr != nil {
				r.log("record failed job %s: %v", job.ID, updateErr)
			}
		} else if err := r.Store.Complete(ctx, job.ID, fileID); err != nil {
			r.log("complete job %s: %v", job.ID, err)
		}
	}
}

func (r Runner) scan(ctx context.Context, root string) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("scan root is not a regular directory: %s", root)
	}
	return r.scanDir(ctx, root)
}

// ReadDir(64) avoids filepath.WalkDir's unbounded per-directory name slice.
func (r Runner) scanDir(ctx context.Context, directory string) error {
	input, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer input.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := input.ReadDir(64)
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				if err := r.scanDir(ctx, path); err != nil {
					return err
				}
				continue
			}
			if !entry.Type().IsRegular() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			for {
				count, err := r.Store.OutstandingFiles(ctx)
				if err != nil {
					return err
				}
				if count < r.QueueLimit {
					break
				}
				if err := pause(ctx, 250*time.Millisecond); err != nil {
					return err
				}
			}
			if _, err := r.Store.Put(ctx, KindFile, path, info.Size(), info.ModTime().UTC(), false); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func pause(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
