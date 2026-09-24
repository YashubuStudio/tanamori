package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
)

type Searcher interface {
	Search(context.Context, string, int) ([]domain.SearchHit, error)
}

func Handler(store jobqueue.Store, searcher Searcher, queueLimit int64) http.Handler {
	mux := http.NewServeMux()
	var enqueueMu sync.Mutex
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		write(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaErr != nil || mediaType != "application/json" {
			problem(w, http.StatusUnsupportedMediaType, fmt.Errorf("application/json is required"))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var request struct {
			Path string `json:"path"`
		}
		if err := decoder.Decode(&request); err != nil {
			problem(w, http.StatusBadRequest, err)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			problem(w, http.StatusBadRequest, fmt.Errorf("one JSON object is required"))
			return
		}
		if strings.TrimSpace(request.Path) == "" {
			problem(w, http.StatusBadRequest, fmt.Errorf("path is required"))
			return
		}
		enqueueMu.Lock()
		job, err := jobqueue.EnqueueLimited(r.Context(), store, request.Path, queueLimit)
		enqueueMu.Unlock()
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
	mux.HandleFunc("GET /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			problem(w, http.StatusNotFound, fmt.Errorf("job not found"))
			return
		}
		write(w, http.StatusOK, job)
	})
	mux.HandleFunc("GET /v1/queue", func(w http.ResponseWriter, r *http.Request) {
		counts, err := store.Counts(r.Context())
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, counts)
	})
	mux.HandleFunc("GET /v1/search", func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			problem(w, http.StatusBadRequest, fmt.Errorf("q is required"))
			return
		}
		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if r.URL.Query().Get("limit") == "" {
			limit = 50
			err = nil
		}
		if err != nil || limit < 1 || limit > 100 {
			problem(w, http.StatusBadRequest, fmt.Errorf("limit must be 1..100"))
			return
		}
		hits, err := searcher.Search(r.Context(), query, limit)
		if err != nil {
			problem(w, http.StatusInternalServerError, err)
			return
		}
		write(w, http.StatusOK, hits)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if parsed, _, err := net.SplitHostPort(host); err == nil {
			host = parsed
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			problem(w, http.StatusForbidden, fmt.Errorf("loopback Host is required"))
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func write(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func problem(w http.ResponseWriter, code int, err error) {
	write(w, code, map[string]string{"error": err.Error()})
}
