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
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/httpapi"
	"github.com/YashubuStudio/tanamori/internal/scoring"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

func TestEmptyUIListsAreJSONArrays(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "empty.db"))
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
	handler := httpapi.UIHandler(db, service, 16, httpapi.AppInfo{})
	for _, path := range []string{"/v1/folders", "/v1/proposals", "/v1/jobs", "/v1/work-logs", "/v1/classifications"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+path, nil))
			if recorder.Code != http.StatusOK || strings.TrimSpace(recorder.Body.String()) != "[]" {
				t.Fatalf("empty list must be [], got status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

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
	shutdownRequested := make(chan struct{}, 1)
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{
		Version: "test", ModelReady: true, ModelID: "test-model",
		Shutdown: func() { shutdownRequested <- struct{}{} },
	}))
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
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("UI must not cache assets from an older application version")
	}
	if !strings.Contains(string(body), "shutdownButton") {
		t.Fatal("UI must include the graceful shutdown button")
	}
	for path, wantStatus := range map[string]int{"/favicon.svg": http.StatusOK, "/favicon.ico": http.StatusNoContent} {
		iconResponse, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		iconResponse.Body.Close()
		if iconResponse.StatusCode != wantStatus {
			t.Fatalf("%s status %d, want %d", path, iconResponse.StatusCode, wantStatus)
		}
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
	unregistered := filepath.Join(root, "未分類.txt")
	if err := os.WriteFile(unregistered, []byte("分類先が未登録の文書"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, unregistered); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"/v1/app", "/v1/queue", "/v1/folders", "/v1/jobs", "/v1/work-logs", "/v1/classifications?limit=100", "/v1/proposals"} {
		result, err := http.Get(server.URL + endpoint)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(result.Body)
		result.Body.Close()
		if result.StatusCode != http.StatusOK {
			t.Fatalf("%s status %d: %s", endpoint, result.StatusCode, data)
		}
		if strings.HasPrefix(endpoint, "/v1/classifications") && (!strings.Contains(string(data), "経費精算.txt") || !strings.Contains(string(data), "未分類.txt")) {
			t.Fatalf("classification missing from response: %s", data)
		}
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("shutdown without confirmation header returned %d", response.StatusCode)
	}
	request, err = http.NewRequest(http.MethodPost, server.URL+"/v1/shutdown", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Tanamori-Action", "shutdown")
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("confirmed shutdown returned %d", response.StatusCode)
	}
	select {
	case <-shutdownRequested:
	case <-time.After(time.Second):
		t.Fatal("shutdown callback was not invoked")
	}
}

func TestResetPreviewAndAutomaticBackupOverHTTP(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	folderPath := filepath.Join(root, "会計")
	if err := os.Mkdir(folderPath, 0o700); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(root, "new.txt")
	if err := os.WriteFile(filePath, []byte("請求書の確認"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "ui.db"))
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
	if _, err := service.RegisterFolder(ctx, folderPath, "会計", "経理文書", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, filePath); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{}))
	defer server.Close()
	response, err := http.Get(server.URL + "/v1/data/preview")
	if err != nil {
		t.Fatal(err)
	}
	var preview domain.ResetPreview
	if err := json.NewDecoder(response.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || preview.Files != 1 || preview.Folders != 1 {
		t.Fatalf("reset preview: status=%d value=%+v", response.StatusCode, preview)
	}
	body, _ := json.Marshal(map[string]any{"documents": true, "folders": true, "confirm": "削除"})
	response, err = http.Post(server.URL+"/v1/data/reset", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var result domain.ResetResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || result.Files != 1 || result.Folders != 1 || result.BackupPath == "" {
		t.Fatalf("reset: status=%d value=%+v", response.StatusCode, result)
	}
	if info, err := os.Stat(result.BackupPath); err != nil || info.Size() == 0 {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestProposeMoveForUnclassifiedFileOverHTTP(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	accounting := filepath.Join(root, "会計")
	if err := os.Mkdir(accounting, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "unsorted.txt")
	if err := os.WriteFile(source, []byte("今日の天気とランチの感想についての雑記。"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "ui.db"))
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
	accountingFolder, err := service.RegisterFolder(ctx, accounting, "会計", "経費精算、請求書、入金管理", "")
	if err != nil {
		t.Fatal(err)
	}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if ingested.Proposal != nil {
		t.Fatalf("expected no automatic proposal: %#v", ingested)
	}
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{}))
	defer server.Close()
	body, _ := json.Marshal(map[string]any{"folder_id": accountingFolder.ID, "score": 0.42, "reason": "手動で選択"})
	response, err := http.Post(server.URL+"/v1/classifications/"+ingested.File.ID+"/propose", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var proposal domain.Proposal
	decodeErr := json.NewDecoder(response.Body).Decode(&proposal)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusCreated || proposal.FolderName != "会計" || proposal.State != domain.ProposalPending {
		t.Fatalf("propose status=%d proposal=%#v decode=%v", response.StatusCode, proposal, decodeErr)
	}
	response, err = http.Post(server.URL+"/v1/classifications/"+ingested.File.ID+"/propose", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected a duplicate proposal to be rejected, got %d", response.StatusCode)
	}
	applyBody, _ := json.Marshal(map[string]any{"reason": "手動で承認"})
	response, err = http.Post(server.URL+"/v1/proposals/"+proposal.ID+"/apply", "application/json", bytes.NewReader(applyBody))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("apply status=%d", response.StatusCode)
	}
}

func TestProposalDirectoriesOverHTTP(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "新着契約.txt")
	if err := os.WriteFile(source, []byte("契約書と更新条件"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "ui.db"))
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
	if _, err := service.RegisterFolder(ctx, destination, "契約書", "契約書", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Ingest(ctx, source); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{}))
	defer server.Close()
	response, err := http.Get(server.URL + "/v1/proposals")
	if err != nil {
		t.Fatal(err)
	}
	var proposals []domain.Proposal
	decodeErr := json.NewDecoder(response.Body).Decode(&proposals)
	response.Body.Close()
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if response.StatusCode != http.StatusOK || len(proposals) != 1 || proposals[0].SourceDirectory != root || proposals[0].DestinationDirectory != destination {
		t.Fatalf("proposal directories: status=%d proposals=%#v", response.StatusCode, proposals)
	}
	response, err = http.Get(server.URL + "/v1/proposals/" + proposals[0].ID + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	var preview app.MovePreview
	decodeErr = json.NewDecoder(response.Body).Decode(&preview)
	response.Body.Close()
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if response.StatusCode != http.StatusOK || preview.SourceDirectory != root || preview.DestinationDirectory != destination {
		t.Fatalf("preview directories: status=%d preview=%#v", response.StatusCode, preview)
	}
}
