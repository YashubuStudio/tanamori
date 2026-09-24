package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/httpapi"
	"github.com/YashubuStudio/tanamori/internal/scoring"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

func TestUploadStreamsToQueueAndNumbersDuplicate(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db, err := storage.Open(ctx, filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	service, err := app.NewService(db, extract.DefaultAdapter(), scoring.KeywordScorer{}, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{UploadRoot: filepath.Join(root, "incoming")}))
	defer server.Close()
	url := server.URL + "/v1/uploads?session=test&path=folder%2Fnote.txt"
	for _, content := range []string{"first", "second"} {
		response, err := http.Post(url, "application/octet-stream", strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("upload status %d: %s", response.StatusCode, body)
		}
	}
	for name, want := range map[string]string{"note.txt": "first", "note (1).txt": "second"} {
		data, err := os.ReadFile(filepath.Join(root, "incoming", "test", "folder", name))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
	response, err := http.Post(server.URL+"/v1/uploads?session=test&path=..%2Fescape.txt", "application/octet-stream", strings.NewReader("bad"))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("traversal status = %d", response.StatusCode)
	}
	resetBody, _ := json.Marshal(map[string]any{"documents": true, "folders": true, "confirm": "削除"})
	response, err = http.Post(server.URL+"/v1/data/reset", "application/json", bytes.NewReader(resetBody))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("reset should wait for queued jobs, got %d", response.StatusCode)
	}
}
