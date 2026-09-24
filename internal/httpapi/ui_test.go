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

func TestUIAndManagementEndpoints(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	accounting := filepath.Join(root, "01_会計")
	meeting := filepath.Join(root, "02_会議")
	for _, path := range []string{accounting, meeting} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	document := filepath.Join(accounting, "経費精算.txt")
	if err := os.WriteFile(document, []byte("交通費と消耗品費の経費精算"), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "ui.db"))
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
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{Version: "test", ModelReady: true, ModelID: "test-model"}))
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "文書の居場所を、ひと目で") {
		t.Fatalf("unexpected UI response: %d %s", response.StatusCode, body)
	}
	if response.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("UI must include a content security policy")
	}

	payload, _ := json.Marshal(map[string]any{"path": root, "enqueue": true})
	response, err = http.Post(server.URL+"/v1/folders/discover", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated {
		failure, _ := io.ReadAll(response.Body)
		t.Fatalf("discover status %d: %s", response.StatusCode, failure)
	}
	response.Body.Close()

	if _, err := service.Ingest(ctx, document); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/v1/app", "/v1/queue", "/v1/folders", "/v1/jobs", "/v1/classifications", "/v1/proposals"} {
		result, err := http.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(result.Body)
		result.Body.Close()
		if result.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d: %s", endpoint, result.StatusCode, data)
		}
		if endpoint == "/v1/classifications" && !strings.Contains(string(data), "経費精算.txt") {
			t.Fatalf("classification missing from response: %s", data)
		}
	}
}
