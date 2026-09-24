package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/httpapi"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

type searcher struct{}

func (searcher) Search(context.Context, string, int) ([]domain.SearchHit, error) {
	return []domain.SearchHit{}, nil
}

func TestHTTPRemainsResponsiveWhileFileIsIngested(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	runner := jobqueue.Runner{Store: db, Workers: 1, QueueLimit: 2,
		Ingest: func(ctx context.Context, path string) (string, error) {
			close(started)
			select {
			case <-release:
				return "fil_done", nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- runner.Run(runCtx) }()
	server := httptest.NewServer(httpapi.Handler(db, db, 2))
	defer server.Close()
	payload, _ := json.Marshal(map[string]string{"path": path})
	response, err := http.Post(server.URL+"/v1/jobs", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("enqueue HTTP status %d", response.StatusCode)
	}
	var job jobqueue.Job
	if err := json.NewDecoder(response.Body).Decode(&job); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	for _, endpoint := range []string{"/healthz", "/v1/queue", "/v1/jobs/" + job.ID, "/v1/search?q=hello"} {
		begin := time.Now()
		result, err := http.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		result.Body.Close()
		if result.StatusCode != http.StatusOK || time.Since(begin) > time.Second {
			t.Fatalf("%s blocked during ingest: status=%d duration=%s", endpoint, result.StatusCode, time.Since(begin))
		}
	}
	close(release)
	deadline := time.After(5 * time.Second)
	for {
		current, err := db.Get(ctx, job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == jobqueue.Succeeded {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("job did not complete: %+v", current)
		case <-time.After(10 * time.Millisecond):
		}
	}
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAppliesBackpressure(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "backpressure.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.Handler(db, searcher{}, 1))
	defer server.Close()
	for index, want := range []int{http.StatusAccepted, http.StatusTooManyRequests} {
		path := filepath.Join(root, fmt.Sprintf("file-%d.txt", index))
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"path": path})
		response, err := http.Post(server.URL+"/v1/jobs", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatalf("submission %d: got %d, want %d", index, response.StatusCode, want)
		}
	}
}

func TestHTTPRejectsNonJSONAndNonLoopbackHost(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "security.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.Handler(db, searcher{}, 1))
	defer server.Close()
	response, err := http.Post(server.URL+"/v1/jobs", "text/plain", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("unexpected content-type status %d", response.StatusCode)
	}
	request, err := http.NewRequest(http.MethodGet, server.URL+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "untrusted.example"
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("unexpected Host status %d", response.StatusCode)
	}
}
