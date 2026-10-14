package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

const maxUploadBytes = 128 << 20

var sessionName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type uploadOriginStore interface {
	RecordUploadOrigin(context.Context, string, string) error
	DeleteUploadOrigin(context.Context, string) error
}

// A dropped browser file has no readable source path, so it is streamed to a
// durable local incoming directory and only that copy is placed on the queue.
func receiveUpload(ctx context.Context, w http.ResponseWriter, r *http.Request, store jobqueue.Store, queueLimit int64, root string) (jobqueue.Job, error) {
	origins, ok := store.(uploadOriginStore)
	if !ok {
		return jobqueue.Job{}, fmt.Errorf("upload origin storage is unavailable")
	}
	if r.ContentLength > maxUploadBytes {
		return jobqueue.Job{}, fmt.Errorf("upload exceeds 128 MiB")
	}
	if queueLimit > 0 {
		outstanding, err := store.OutstandingFiles(ctx)
		if err != nil {
			return jobqueue.Job{}, err
		}
		if outstanding >= queueLimit {
			return jobqueue.Job{}, jobqueue.ErrQueueFull
		}
	}
	session := r.URL.Query().Get("session")
	relative := filepath.FromSlash(r.URL.Query().Get("path"))
	if !sessionName.MatchString(session) || !filepath.IsLocal(relative) || filepath.Base(relative) == "." {
		return jobqueue.Job{}, fmt.Errorf("invalid upload path or session")
	}
	for _, segment := range strings.Split(filepath.ToSlash(relative), "/") {
		if segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || strings.ContainsAny(segment, `:*?"<>|`) {
			return jobqueue.Job{}, fmt.Errorf("invalid Windows filename")
		}
	}
	directory := filepath.Join(root, session, filepath.Dir(relative))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return jobqueue.Job{}, err
	}
	temporary, err := os.CreateTemp(directory, ".tanamori-upload-*")
	if err != nil {
		return jobqueue.Job{}, err
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	limited := http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if _, err := io.Copy(temporary, limited); err != nil {
		return jobqueue.Job{}, err
	}
	if err := temporary.Sync(); err != nil {
		return jobqueue.Job{}, err
	}
	if err := temporary.Close(); err != nil {
		return jobqueue.Job{}, err
	}
	extension := filepath.Ext(relative)
	stem := strings.TrimSuffix(filepath.Base(relative), extension)
	for number := 0; number <= 10000; number++ {
		name := filepath.Base(relative)
		if number > 0 {
			name = stem + " (" + strconv.Itoa(number) + ")" + extension
		}
		target := filepath.Join(directory, name)
		if err := os.Link(temporary.Name(), target); err != nil {
			if _, statErr := os.Lstat(target); statErr == nil {
				continue
			}
			if err := copyUploadExclusive(temporary.Name(), target); err != nil {
				if errors.Is(err, os.ErrExist) {
					continue
				}
				return jobqueue.Job{}, fmt.Errorf("commit uploaded file: %w", err)
			}
		}
		if err := origins.RecordUploadOrigin(ctx, target, relative); err != nil {
			_ = os.Remove(target)
			return jobqueue.Job{}, err
		}
		job, err := jobqueue.EnqueueLimited(ctx, store, target, queueLimit)
		if err != nil {
			_ = origins.DeleteUploadOrigin(ctx, target)
			_ = os.Remove(target)
			return jobqueue.Job{}, err
		}
		return job, nil
	}
	return jobqueue.Job{}, errors.New("too many duplicate upload names")
}

func copyUploadExclusive(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		_ = output.Close()
		if !complete {
			_ = os.Remove(destination)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	complete = true
	return nil
}
