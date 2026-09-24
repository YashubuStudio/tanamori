package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/httpapi"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
	"github.com/YashubuStudio/tanamori/internal/modelsetup"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

func runServer(ctx context.Context, db *storage.DB, service *app.Service, address string, requestedWorkers int, queueLimit int64, openUI bool, model modelsetup.Status, stderr io.Writer) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return errors.New("HTTP server may listen only on localhost or a loopback IP")
	}
	if queueLimit < 1 || queueLimit > 5000 {
		return errors.New("queue limit must be 1..5000")
	}
	if requestedWorkers < 0 {
		return errors.New("workers must not be negative")
	}
	workers := jobqueue.Capacity(runtime.NumCPU(), jobqueue.AvailableMemory(), requestedWorkers)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	logger := log.New(stderr, "tanamori: ", log.LstdFlags)
	runner := jobqueue.Runner{
		Store: db, Workers: workers, QueueLimit: queueLimit,
		Ingest: func(ctx context.Context, path string) (string, error) { return ingestQueued(ctx, service, path) },
		Log:    logger.Printf,
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workerDone := make(chan error, 1)
	go func() { workerDone <- runner.Run(workCtx) }()
	server := &http.Server{
		Handler: httpapi.UIHandler(db, service, queueLimit, httpapi.AppInfo{
			Version: version, ModelReady: model.Ready, ModelID: model.ModelID,
			ModelMessage: model.Message, Workers: workers, QueueLimit: queueLimit,
			TestPath:   nearbyTestPath(),
			UploadRoot: filepath.Join(filepath.Dir(db.Path()), "incoming"),
		}),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 2 * time.Minute,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second,
		MaxHeaderBytes: 8192,
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(listener) }()
	logger.Printf("HTTP ready at http://%s (file workers=%d, queue limit=%d)", listener.Addr(), workers, queueLimit)
	if openUI {
		if err := launchBrowser("http://" + listener.Addr().String()); err != nil {
			logger.Printf("open browser: %v; open the URL manually", err)
		}
	}
	var result error
	workerFinished := false
	select {
	case err := <-workerDone:
		workerFinished = true
		result = err
	case err := <-serverDone:
		if !errors.Is(err, http.ErrServerClosed) {
			result = err
		}
	case <-ctx.Done():
	}
	cancel()
	shutdownCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	if err := server.Shutdown(shutdownCtx); err != nil && result == nil {
		result = err
	}
	if !workerFinished {
		if err := <-workerDone; err != nil && result == nil {
			result = err
		}
	}
	return result
}

func launchBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}

func nearbyTestPath() string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "TANAMORI_分類テスト"))
	info, err := os.Stat(candidate)
	if err == nil && info.IsDir() {
		return candidate
	}
	return ""
}

func ingestQueued(ctx context.Context, service *app.Service, path string) (string, error) {
	result, err := service.Ingest(ctx, path)
	if err != nil {
		return "", err
	}
	if result.File.ExtractStatus != "ok" {
		return result.File.ID, fmt.Errorf("extraction failed: %s", result.File.ExtractError)
	}
	for _, run := range result.ModelRuns {
		if run.Status != "ok" {
			return result.File.ID, fmt.Errorf("model scoring failed: %s", run.Error)
		}
	}
	return result.File.ID, nil
}
