package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestBrowserUploadRelativePathCanBeReusedAfterMove(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "library.db"))
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
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{UploadRoot: filepath.Join(root, "incoming")}))
	defer server.Close()
	relative := "folder/新着契約.txt"
	uploadOnce := func(content string) app.MoveResult {
		response, err := http.Post(server.URL+"/v1/uploads?session=test&path="+url.QueryEscape(relative), "application/octet-stream", strings.NewReader(content))
		if err != nil {
			t.Fatal(err)
		}
		var uploaded struct {
			Path string `json:"path"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&uploaded)
		response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusAccepted {
			t.Fatalf("upload status=%d body=%+v decode=%v", response.StatusCode, uploaded, decodeErr)
		}
		if _, err := service.Ingest(ctx, uploaded.Path); err != nil {
			t.Fatal(err)
		}
		proposals, err := service.ListProposals(ctx, domain.ProposalPending, 10)
		if err != nil || len(proposals) != 1 {
			t.Fatalf("expected one pending proposal, got %+v, err=%v", proposals, err)
		}
		result, err := service.ApplyMoveProposal(ctx, proposals[0].ID, domain.ActorUser, "test", app.MoveOptions{OnConflict: "rename"})
		if err != nil {
			t.Fatalf("apply move proposal: %v", err)
		}
		return result
	}
	first := uploadOnce("契約書と更新条件 第一版")
	second := uploadOnce("契約書と更新条件 第二版")
	if first.ToPath == second.ToPath {
		t.Fatalf("expected distinct destinations, got %q twice", first.ToPath)
	}
	for _, path := range []string{first.ToPath, second.ToPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected moved file at %q: %v", path, err)
		}
	}
}

func TestBrowserUploadOriginIsNotPresentedAsOriginalDirectory(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	destination := filepath.Join(root, "契約書")
	if err := os.Mkdir(destination, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(ctx, filepath.Join(root, "library.db"))
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
	server := httptest.NewServer(httpapi.UIHandler(db, service, 16, httpapi.AppInfo{UploadRoot: filepath.Join(root, "incoming")}))
	defer server.Close()
	relative := "folder/新着契約.txt"
	response, err := http.Post(server.URL+"/v1/uploads?session=test&path="+url.QueryEscape(relative), "application/octet-stream", strings.NewReader("契約書と更新条件"))
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		Path string `json:"path"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&uploaded)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("upload status=%d body=%+v decode=%v", response.StatusCode, uploaded, decodeErr)
	}
	if _, err := service.Ingest(ctx, uploaded.Path); err != nil {
		t.Fatal(err)
	}
	response, err = http.Get(server.URL + "/v1/proposals")
	if err != nil {
		t.Fatal(err)
	}
	var proposals []domain.Proposal
	decodeErr = json.NewDecoder(response.Body).Decode(&proposals)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || len(proposals) != 1 {
		t.Fatalf("proposals status=%d proposals=%+v decode=%v", response.StatusCode, proposals, decodeErr)
	}
	proposal := proposals[0]
	if proposal.SourceKind != domain.SourceBrowserUpload || proposal.SourceDirectory != "" || proposal.SourceRelativePath != relative || proposal.WorkingCopyDirectory != filepath.Dir(uploaded.Path) || proposal.DestinationDirectory != destination {
		t.Fatalf("incorrect browser source: %+v", proposal)
	}
	response, err = http.Get(server.URL + "/v1/proposals/" + proposal.ID + "/preview")
	if err != nil {
		t.Fatal(err)
	}
	var preview app.MovePreview
	decodeErr = json.NewDecoder(response.Body).Decode(&preview)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || preview.SourceKind != domain.SourceBrowserUpload || preview.SourceDirectory != "" || preview.SourceRelativePath != relative || preview.WorkingCopyDirectory != filepath.Dir(uploaded.Path) {
		t.Fatalf("incorrect preview source: status=%d preview=%+v decode=%v", response.StatusCode, preview, decodeErr)
	}
	response, err = http.Get(server.URL + "/v1/jobs")
	if err != nil {
		t.Fatal(err)
	}
	var jobs []struct {
		SourceKind         string `json:"source_kind"`
		SourceRelativePath string `json:"source_relative_path"`
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&jobs)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || len(jobs) != 1 || jobs[0].SourceKind != domain.SourceBrowserUpload || jobs[0].SourceRelativePath != relative {
		t.Fatalf("incorrect job source: status=%d jobs=%+v decode=%v", response.StatusCode, jobs, decodeErr)
	}
	response, err = http.Get(server.URL + "/v1/work-logs")
	if err != nil {
		t.Fatal(err)
	}
	var logs []struct {
		EventType          string `json:"event_type"`
		SourceKind         string `json:"source_kind"`
		SourceRelativePath string `json:"source_relative_path"`
	}
	decodeErr = json.NewDecoder(response.Body).Decode(&logs)
	response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || len(logs) != 1 || logs[0].EventType != "JOB_QUEUED" || logs[0].SourceKind != domain.SourceBrowserUpload || logs[0].SourceRelativePath != relative {
		t.Fatalf("incorrect upload work log: status=%d logs=%+v decode=%v", response.StatusCode, logs, decodeErr)
	}
	if err := db.DeleteUploadOrigin(ctx, uploaded.Path); err != nil {
		t.Fatal(err)
	}
	legacy, err := service.ListProposals(ctx, domain.ProposalPending, 10)
	if err != nil || len(legacy) != 1 || legacy[0].SourceKind != domain.SourceBrowserUpload || legacy[0].SourceDirectory != "" || legacy[0].SourceRelativePath != "" {
		t.Fatalf("legacy upload must not be mistaken for original: %+v %v", legacy, err)
	}
}
