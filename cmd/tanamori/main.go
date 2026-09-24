package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/jobqueue"
	"github.com/YashubuStudio/tanamori/internal/modelsetup"
	"github.com/YashubuStudio/tanamori/internal/scoring"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

const version = "0.5.0-web"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		args = []string{"web"}
	}
	os.Exit(run(args, os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command := args[0]
	if command == "version" {
		fmt.Fprintln(stdout, "tanamori "+version)
		return 0
	}

	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db", defaultDBPath(), "SQLite library database path")
	path := flags.String("path", "", "file or folder path")
	name := flags.String("name", "", "folder display name")
	purpose := flags.String("purpose", "", "folder purpose")
	description := flags.String("description", "", "folder description")
	query := flags.String("query", "", "search query")
	proposalID := flags.String("proposal", "", "proposal ID")
	reason := flags.String("reason", "", "decision reason")
	state := flags.String("state", string(domain.ProposalPending), "proposal state")
	limit := flags.Int("limit", 50, "maximum result count")
	jobID := flags.String("job", "", "ingestion job ID")
	listen := flags.String("listen", "127.0.0.1:8765", "loopback HTTP listen address")
	workers := flags.Int("workers", 0, "file workers; 0 selects hardware limit")
	queueLimit := flags.Int64("queue-limit", 256, "maximum queued and running file jobs discovered by a directory scan")
	modelRoot := flags.String("model-root", preferredModelRoot(), "per-user AI model and runtime directory")
	python := flags.String("python", "", "64-bit Python 3.10+ executable used during model setup")
	force := flags.Bool("force", false, "refresh the isolated runtime and model snapshot")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	installer, err := modelsetup.New(*modelRoot)
	if err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}
	installer.Python = *python
	installer.Stdout = stderr
	installer.Stderr = stderr
	switch command {
	case "model-status":
		status, statusErr := installer.Status()
		if statusErr != nil {
			fmt.Fprintln(stderr, "tanamori:", statusErr)
			return 1
		}
		return writeJSON(stdout, stderr, status)
	case "model-install":
		status, installErr := installer.Install(ctx, *force)
		if installErr != nil {
			fmt.Fprintln(stderr, "tanamori:", installErr)
			return 1
		}
		return writeJSON(stdout, stderr, status)
	case "model-verify":
		status, verifyErr := installer.Verify(ctx)
		if verifyErr != nil {
			fmt.Fprintln(stderr, "tanamori:", verifyErr)
			return 1
		}
		return writeJSON(stdout, stderr, status)
	}

	db, err := storage.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}
	if command == "init" {
		return writeJSON(stdout, stderr, map[string]string{"database": *dbPath, "status": "ready"})
	}
	if command == "enqueue" {
		if strings.TrimSpace(*path) == "" {
			fmt.Fprintln(stderr, "tanamori: --path is required")
			return 2
		}
		job, enqueueErr := jobqueue.EnqueueLimited(ctx, db, *path, *queueLimit)
		if enqueueErr != nil {
			fmt.Fprintln(stderr, "tanamori:", enqueueErr)
			return 1
		}
		return writeJSON(stdout, stderr, job)
	}
	if command == "queue-status" {
		counts, countErr := db.Counts(ctx)
		if countErr != nil {
			fmt.Fprintln(stderr, "tanamori:", countErr)
			return 1
		}
		return writeJSON(stdout, stderr, counts)
	}
	if command == "job-status" {
		if strings.TrimSpace(*jobID) == "" {
			fmt.Fprintln(stderr, "tanamori: --job is required")
			return 2
		}
		job, jobErr := db.Get(ctx, *jobID)
		if jobErr != nil {
			fmt.Fprintln(stderr, "tanamori:", jobErr)
			return 1
		}
		return writeJSON(stdout, stderr, job)
	}
	if command == "setup" {
		status, installErr := installer.Install(ctx, *force)
		if installErr != nil {
			fmt.Fprintln(stderr, "tanamori:", installErr)
			return 1
		}
		return writeJSON(stdout, stderr, map[string]any{"database": *dbPath, "model": status, "status": "ready"})
	}
	modelStatus, _ := installer.Status()
	mainScorer, closer := configuredScorer(installer, command, stderr)
	if closer != nil {
		defer closer.Close()
	}
	localFS := filesystem.Local{}
	if command == "serve" || command == "web" {
		localFS.MaxFileBytes = 512 << 20
	}
	service, err := app.NewService(db, extract.DefaultAdapter(), mainScorer, localFS, domain.DefaultClassificationPolicy())
	if err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}

	var value any
	switch command {
	case "serve", "web":
		if err := runServer(ctx, db, service, *listen, *workers, *queueLimit, command == "web", modelStatus, stderr); err != nil {
			fmt.Fprintln(stderr, "tanamori:", err)
			return 1
		}
		return 0
	case "folder-add":
		if strings.TrimSpace(*path) == "" {
			fmt.Fprintln(stderr, "tanamori: --path is required")
			return 2
		}
		value, err = service.RegisterFolder(ctx, *path, *name, *purpose, *description)
	case "folder-list":
		value, err = service.ListFolders(ctx)
	case "ingest":
		if strings.TrimSpace(*path) == "" {
			fmt.Fprintln(stderr, "tanamori: --path is required")
			return 2
		}
		value, err = service.Ingest(ctx, *path)
	case "search":
		if strings.TrimSpace(*query) == "" {
			fmt.Fprintln(stderr, "tanamori: --query is required")
			return 2
		}
		value, err = service.Search(ctx, *query, *limit)
	case "proposals":
		value, err = service.ListProposals(ctx, domain.ProposalState(*state), *limit)
	case "apply":
		if strings.TrimSpace(*proposalID) == "" {
			fmt.Fprintln(stderr, "tanamori: --proposal is required")
			return 2
		}
		value, err = service.ApplyMoveProposal(ctx, *proposalID, domain.ActorUser, *reason)
	default:
		fmt.Fprintf(stderr, "tanamori: unknown command %q\n", command)
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}
	return writeJSON(stdout, stderr, value)
}

func configuredScorer(installer *modelsetup.Installer, command string, stderr io.Writer) (app.Scorer, io.Closer) {
	if command == "serve" || command == "web" {
		available := jobqueue.AvailableMemory()
		if available > 0 && available < 5<<30 {
			fmt.Fprintf(stderr, "tanamori: available RAM %d GiB is below BGE server safety floor; using keyword fallback\n", available>>30)
			return scoring.KeywordScorer{}, nil
		}
	}
	status, err := installer.Status()
	if err == nil && status.Ready {
		reranker, rerankerErr := scoring.NewBGEReranker(scoring.BGEConfig{
			ModelID: installer.Manifest.ModelID, Revision: installer.Manifest.Revision,
			ModelDir: installer.Paths.ModelDir, Python: installer.Paths.Python, Runner: installer.Paths.Runner,
		})
		if rerankerErr == nil {
			return reranker, reranker
		}
		err = rerankerErr
	}
	if command == "ingest" || command == "serve" || command == "web" {
		if err != nil {
			fmt.Fprintf(stderr, "tanamori: BGE model unavailable (%v); using keyword fallback\n", err)
		} else {
			fmt.Fprintln(stderr, "tanamori: BGE model is not installed; using keyword fallback (run `tanamori setup`)")
		}
	}
	return scoring.KeywordScorer{}, nil
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, "tanamori:", err)
		return 1
	}
	return 0
}

func defaultDBPath() string {
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "YashubuStudio", "TANAMORI", "library.db")
	}
	if config, err := os.UserConfigDir(); err == nil {
		return filepath.Join(config, "YashubuStudio", "TANAMORI", "library.db")
	}
	return "library.db"
}

func preferredModelRoot() string {
	fallback := modelsetup.DefaultRoot()
	executable, err := os.Executable()
	if err != nil {
		return fallback
	}
	candidate := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", "data", "ai"))
	installer, err := modelsetup.New(candidate)
	if err != nil {
		return fallback
	}
	status, err := installer.Status()
	if err == nil && status.Ready {
		return candidate
	}
	return fallback
}

func usage(output io.Writer) {
	fmt.Fprintln(output, "Usage: tanamori COMMAND [options]")
	fmt.Fprintln(output, "Commands:")
	fmt.Fprintln(output, "  web          open the local graphical Web UI")
	fmt.Fprintln(output, "  setup        first-run database and BGE model setup")
	fmt.Fprintln(output, "  init         create or migrate the library database")
	fmt.Fprintln(output, "  folder-add   register a managed destination folder")
	fmt.Fprintln(output, "  folder-list  list managed folders")
	fmt.Fprintln(output, "  ingest       extract and index one file")
	fmt.Fprintln(output, "  enqueue      queue a file or directory for background ingestion")
	fmt.Fprintln(output, "  serve        run loopback HTTP API and bounded queue workers")
	fmt.Fprintln(output, "  queue-status show durable queue counts")
	fmt.Fprintln(output, "  job-status   show one ingestion job (--job ID)")
	fmt.Fprintln(output, "  search       search indexed files with SQLite FTS5")
	fmt.Fprintln(output, "  proposals    list proposals (pending by default)")
	fmt.Fprintln(output, "  apply        approve and safely apply a move proposal")
	fmt.Fprintln(output, "  model-status show local BGE model readiness")
	fmt.Fprintln(output, "  model-install install or update the local BGE model")
	fmt.Fprintln(output, "  model-verify verify the complete model weight SHA-256")
	fmt.Fprintln(output, "  version      print the application version")
}
