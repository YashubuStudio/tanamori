package httpapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

//go:embed ui/*
var embeddedUI embed.FS

type AppInfo struct {
	Version      string `json:"version"`
	ModelReady   bool   `json:"model_ready"`
	ModelID      string `json:"model_id"`
	ModelMessage string `json:"model_message,omitempty"`
	Workers      int    `json:"workers"`
	QueueLimit   int64  `json:"queue_limit"`
	TestPath     string `json:"test_path,omitempty"`
	UploadRoot   string `json:"-"`
}

type recentJobLister interface {
	ListRecentJobs(context.Context, int) ([]jobqueue.Job, error)
}

type classificationLister interface {
	ListClassifications(context.Context, int) ([]domain.ClassificationSummary, error)
}

var orderingPrefix = regexp.MustCompile(`^\s*\d+[\s_.-]+`)

// UIHandler adds an embedded operator UI and management endpoints to Handler.
// It remains loopback-only and exposes no file body through its view endpoints.
func UIHandler(store jobqueue.Store, manager *app.Service, queueLimit int64, info AppInfo) http.Handler {
	base := Handler(store, manager, queueLimit)
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/app", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, info)
	})
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		settings, err := manager.GetSettings(r.Context())
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, settings)
	})
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var settings domain.Settings
		if err := decodeRequest(w, r, &settings); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		if err := manager.SaveSettings(r.Context(), settings); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, settings)
	})
	mux.HandleFunc("POST /v1/data/reset", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Documents bool   `json:"documents"`
			Folders   bool   `json:"folders"`
			Confirm   string `json:"confirm"`
		}
		if err := decodeRequest(w, r, &request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		if request.Confirm != "削除" {
			problem(w, http.StatusBadRequest, fmt.Errorf("confirmation text must be 削除"))
			return
		}
		result, err := manager.ResetData(r.Context(), request.Documents, request.Folders)
		if err != nil {
			problem(w, http.StatusConflict, err)
			return
		}
		write(w, http.StatusOK, result)
	})
	mux.HandleFunc("POST /v1/uploads", func(w http.ResponseWriter, r *http.Request) {
		if info.UploadRoot == "" {
			problem(w, http.StatusServiceUnavailable, fmt.Errorf("uploads are not configured"))
			return
		}
		job, err := receiveUpload(r.Context(), w, r, store, queueLimit, info.UploadRoot)
		if err != nil {
			if errors.Is(err, jobqueue.ErrQueueFull) {
				problem(w, http.StatusTooManyRequests, err)
				return
			}
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusAccepted, job)
	})
	mux.HandleFunc("GET /v1/folders", func(w http.ResponseWriter, r *http.Request) {
		folders, err := manager.ListFolders(r.Context())
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, folders)
	})
	mux.HandleFunc("POST /v1/folders", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Path        string `json:"path"`
			Name        string `json:"name"`
			Purpose     string `json:"purpose"`
			Description string `json:"description"`
		}
		if err := decodeRequest(w, r, &request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		folder, err := manager.RegisterFolder(r.Context(), request.Path, request.Name, request.Purpose, request.Description)
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusCreated, folder)
	})
	mux.HandleFunc("POST /v1/folders/discover", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Path    string `json:"path"`
			Enqueue bool   `json:"enqueue"`
		}
		if err := decodeRequest(w, r, &request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		folders, jobs, err := discoverFolders(r.Context(), store, manager, request.Path, request.Enqueue, queueLimit)
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusCreated, map[string]any{"folders": folders, "jobs": jobs})
	})
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		lister, ok := store.(recentJobLister)
		if !ok {
			problem(w, http.StatusNotImplemented, fmt.Errorf("recent jobs are unavailable"))
			return
		}
		limit, err := requestLimit(r, 50, 200)
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		jobs, err := lister.ListRecentJobs(r.Context(), limit)
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, jobs)
	})
	mux.HandleFunc("GET /v1/classifications", func(w http.ResponseWriter, r *http.Request) {
		lister, ok := store.(classificationLister)
		if !ok {
			problem(w, http.StatusNotImplemented, fmt.Errorf("classification view is unavailable"))
			return
		}
		limit, err := requestLimit(r, 50, 200)
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		items, err := lister.ListClassifications(r.Context(), limit)
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /v1/proposals", func(w http.ResponseWriter, r *http.Request) {
		state := domain.ProposalState(strings.TrimSpace(r.URL.Query().Get("state")))
		if state == "" {
			state = domain.ProposalPending
		}
		limit, err := requestLimit(r, 50, 200)
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		items, err := manager.ListProposals(r.Context(), state, limit)
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, items)
	})
	mux.HandleFunc("GET /v1/proposals/{id}/preview", func(w http.ResponseWriter, r *http.Request) {
		preview, err := manager.PreviewMoveProposal(r.Context(), r.PathValue("id"))
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, preview)
	})
	mux.HandleFunc("POST /v1/proposals/{id}/reject", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Reason string `json:"reason"`
		}
		if err := decodeRequest(w, r, &request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		if err := manager.RejectMoveProposal(r.Context(), r.PathValue("id"), request.Reason); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, map[string]string{"state": "rejected"})
	})
	mux.HandleFunc("POST /v1/proposals/{id}/apply", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Reason        string `json:"reason"`
			OnConflict    string `json:"on_conflict"`
			RenamePattern string `json:"rename_pattern"`
		}
		if err := decodeRequest(w, r, &request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		result, err := manager.ApplyMoveProposal(r.Context(), r.PathValue("id"), domain.ActorUser, request.Reason,
			app.MoveOptions{OnConflict: request.OnConflict, RenamePattern: request.RenamePattern})
		if err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		write(w, http.StatusOK, result)
	})

	root, err := fs.Sub(embeddedUI, "ui")
	if err != nil {
		panic(err)
	}
	static := http.FileServer(http.FS(root))
	mux.Handle("GET /{$}", static)
	mux.Handle("GET /styles.css", static)
	mux.Handle("GET /app.js", static)
	mux.Handle("/", base)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			problem(w, http.StatusForbidden, fmt.Errorf("loopback Host is required"))
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		mux.ServeHTTP(w, r)
	})
}

func decodeRequest(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if mediaType != "application/json" {
		return fmt.Errorf("application/json is required")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("one JSON object is required")
	}
	return nil
}

func requestLimit(r *http.Request, fallback, maximum int) (int, error) {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maximum {
		return 0, fmt.Errorf("limit must be 1..%d", maximum)
	}
	return limit, nil
}

func loopbackHost(host string) bool {
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func discoverFolders(ctx context.Context, store jobqueue.Store, manager *app.Service, rootPath string, enqueue bool, queueLimit int64) ([]domain.Folder, []jobqueue.Job, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(rootPath))
	if err != nil {
		return nil, nil, fmt.Errorf("resolve parent folder: %w", err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect parent folder: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("path must be a regular directory")
	}
	handle, err := os.Open(absolute)
	if err != nil {
		return nil, nil, err
	}
	defer handle.Close()
	folders := make([]domain.Folder, 0)
	jobs := make([]jobqueue.Job, 0)
	for {
		entries, readErr := handle.ReadDir(64)
		for _, entry := range entries {
			if len(folders) >= 500 {
				return nil, nil, fmt.Errorf("at most 500 direct child folders can be registered at once")
			}
			if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			name := strings.TrimSpace(orderingPrefix.ReplaceAllString(entry.Name(), ""))
			if name == "" {
				name = entry.Name()
			}
			path := filepath.Join(absolute, entry.Name())
			folder, registerErr := manager.RegisterFolder(ctx, path, name, name+"に関する文書・資料", "親フォルダーから一括登録")
			if registerErr != nil {
				return nil, nil, registerErr
			}
			folders = append(folders, folder)
			if enqueue {
				job, enqueueErr := jobqueue.EnqueueLimited(ctx, store, path, queueLimit)
				if enqueueErr != nil && !errors.Is(enqueueErr, jobqueue.ErrQueueFull) {
					return nil, nil, enqueueErr
				}
				if enqueueErr == nil {
					jobs = append(jobs, job)
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, nil, readErr
		}
	}
	if len(folders) == 0 {
		return nil, nil, fmt.Errorf("no direct child folders were found")
	}
	return folders, jobs, nil
}
