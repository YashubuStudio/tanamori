package scoring

import (
	"bufio"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/YashubuStudio/tanamori/internal/app"
)

const (
	maxQueryRunes   = 4 * 1024
	maxPassageRunes = 32 * 1024
)

// BGEConfig points at a model runtime produced by modelsetup.Installer.
type BGEConfig struct {
	ModelID         string
	Revision        string
	ModelDir        string
	Python          string
	Runner          string
	InterpreterArgs []string
	Environment     []string
}

// BGEReranker is a lazy, persistent adapter for BAAI/bge-reranker-v2-m3.
// A single Python worker is retained for the command lifetime so the 0.6B
// parameter model is loaded only once. Requests are serialized deliberately:
// this limits peak memory and matches TANAMORI's current ingestion pipeline.
type BGEReranker struct {
	config BGEConfig

	mu      sync.Mutex
	command *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  tailBuffer
	nextID  uint64
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (buffer *tailBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	const limit = 32 * 1024
	if len(value) >= limit {
		buffer.data = append(buffer.data[:0], value[len(value)-limit:]...)
		return len(value), nil
	}
	if overflow := len(buffer.data) + len(value) - limit; overflow > 0 {
		copy(buffer.data, buffer.data[overflow:])
		buffer.data = buffer.data[:len(buffer.data)-overflow]
	}
	buffer.data = append(buffer.data, value...)
	return len(value), nil
}

func (buffer *tailBuffer) Reset() {
	buffer.mu.Lock()
	buffer.data = buffer.data[:0]
	buffer.mu.Unlock()
}

func (buffer *tailBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.data)
}

type workerMessage struct {
	ID         string  `json:"id,omitempty"`
	Type       string  `json:"type,omitempty"`
	ModelID    string  `json:"model_id,omitempty"`
	Revision   string  `json:"revision,omitempty"`
	Device     string  `json:"device,omitempty"`
	Score      float64 `json:"score,omitempty"`
	Logit      float64 `json:"logit,omitempty"`
	Chunks     int     `json:"chunks,omitempty"`
	DurationMS float64 `json:"duration_ms,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type workerRequest struct {
	ID      string `json:"id"`
	Command string `json:"command,omitempty"`
	Query   string `json:"query,omitempty"`
	Passage string `json:"passage,omitempty"`
}

// NewBGEReranker validates the small runtime files. Model loading remains lazy
// so commands that do not classify a file stay fast.
func NewBGEReranker(config BGEConfig) (*BGEReranker, error) {
	for name, value := range map[string]string{
		"model ID": config.ModelID, "revision": config.Revision, "model directory": config.ModelDir,
		"Python": config.Python, "runtime": config.Runner,
	} {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("BGE %s is required", name)
		}
	}
	for name, path := range map[string]string{"Python": config.Python, "runtime": config.Runner, "model directory": config.ModelDir} {
		if _, err := os.Stat(path); err != nil {
			return nil, fmt.Errorf("inspect BGE %s: %w", name, err)
		}
	}
	return &BGEReranker{config: config}, nil
}

// Score treats the extracted file text as the retrieval query and the concise
// folder description as the candidate passage. This matches the reranker's
// query/passage semantics and produces well-separated folder relevance scores.
func (*BGEReranker) SupportsSemanticTags() bool { return true }

func (reranker *BGEReranker) Score(ctx context.Context, request app.ScoreRequest) (app.ScoreResult, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return app.ScoreResult{}, err
	}
	reranker.mu.Lock()
	defer reranker.mu.Unlock()
	if err := reranker.startLocked(ctx); err != nil {
		return app.ScoreResult{}, err
	}
	reranker.nextID++
	id := fmt.Sprintf("score-%d", reranker.nextID)
	requestMessage := workerRequest{
		ID:      id,
		Query:   clipText(request.LeftText, maxPassageRunes),
		Passage: clipText(request.RightText, maxQueryRunes),
	}
	if err := json.NewEncoder(reranker.stdin).Encode(requestMessage); err != nil {
		reranker.stopLocked()
		return app.ScoreResult{}, fmt.Errorf("send request to BGE worker: %w", err)
	}
	response, err := reranker.readLocked(ctx)
	if err != nil {
		return app.ScoreResult{}, err
	}
	if response.ID != id {
		reranker.stopLocked()
		return app.ScoreResult{}, fmt.Errorf("BGE worker protocol mismatch: got response %q, want %q", response.ID, id)
	}
	if response.Error != "" {
		return app.ScoreResult{}, errors.New(response.Error)
	}
	if math.IsNaN(response.Score) || math.IsInf(response.Score, 0) || response.Score < 0 || response.Score > 1 {
		reranker.stopLocked()
		return app.ScoreResult{}, fmt.Errorf("BGE worker returned invalid score %v", response.Score)
	}
	duration := time.Since(started)
	if response.DurationMS > 0 {
		duration = time.Duration(response.DurationMS * float64(time.Millisecond))
	}
	reason := fmt.Sprintf("bge-reranker-v2-m3 relevance %.3f", response.Score)
	if response.Chunks > 0 {
		reason += fmt.Sprintf(" (%d chunk)", response.Chunks)
	}
	return app.ScoreResult{
		Score: response.Score, Reason: reason,
		ModelVersion: reranker.config.ModelID + "@" + reranker.config.Revision,
		Duration:     duration,
	}, nil
}

func (reranker *BGEReranker) startLocked(ctx context.Context) error {
	if reranker.command != nil {
		return nil
	}
	reranker.stderr.Reset()
	arguments := append([]string(nil), reranker.config.InterpreterArgs...)
	arguments = append(arguments,
		reranker.config.Runner,
		"serve",
		"--model-id", reranker.config.ModelID,
		"--revision", reranker.config.Revision,
		"--model-dir", reranker.config.ModelDir,
	)
	command := exec.Command(reranker.config.Python, arguments...)
	if len(reranker.config.Environment) > 0 {
		command.Env = append(os.Environ(), reranker.config.Environment...)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		return fmt.Errorf("open BGE worker input: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("open BGE worker output: %w", err)
	}
	command.Stderr = &reranker.stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("start BGE worker: %w", err)
	}
	reranker.command = command
	reranker.stdin = stdin
	reranker.scanner = bufio.NewScanner(stdout)
	reranker.scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	ready, err := reranker.readLocked(ctx)
	if err != nil {
		return fmt.Errorf("BGE worker did not become ready: %w", err)
	}
	if ready.Type != "ready" {
		reranker.stopLocked()
		return fmt.Errorf("BGE worker did not report readiness%s", reranker.stderrSuffix())
	}
	if ready.ModelID != reranker.config.ModelID || ready.Revision != reranker.config.Revision {
		reranker.stopLocked()
		return fmt.Errorf("BGE worker loaded an unexpected model revision")
	}
	return nil
}

func (reranker *BGEReranker) readLocked(ctx context.Context) (workerMessage, error) {
	type result struct {
		message workerMessage
		err     error
	}
	completed := make(chan result, 1)
	go func() {
		if !reranker.scanner.Scan() {
			err := reranker.scanner.Err()
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			completed <- result{err: err}
			return
		}
		var message workerMessage
		err := json.Unmarshal(reranker.scanner.Bytes(), &message)
		completed <- result{message: message, err: err}
	}()
	select {
	case value := <-completed:
		if value.err != nil {
			reranker.stopLocked()
			return workerMessage{}, fmt.Errorf("read BGE worker response: %w%s", value.err, reranker.stderrSuffix())
		}
		return value.message, nil
	case <-ctx.Done():
		reranker.stopLocked()
		return workerMessage{}, ctx.Err()
	}
}

func (reranker *BGEReranker) stderrSuffix() string {
	message := strings.TrimSpace(reranker.stderr.String())
	if message == "" {
		return ""
	}
	const maximum = 2048
	if len(message) > maximum {
		message = message[len(message)-maximum:]
	}
	return ": " + message
}

func (reranker *BGEReranker) stopLocked() {
	if reranker.stdin != nil {
		_ = reranker.stdin.Close()
	}
	if reranker.command != nil && reranker.command.Process != nil {
		_ = reranker.command.Process.Kill()
		_ = reranker.command.Wait()
	}
	reranker.command = nil
	reranker.stdin = nil
	reranker.scanner = nil
}

// Close releases the persistent model worker and its memory.
func (reranker *BGEReranker) Close() error {
	reranker.mu.Lock()
	defer reranker.mu.Unlock()
	reranker.stopLocked()
	return nil
}

func clipText(value string, maximum int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= maximum {
		return string(runes)
	}
	head := maximum * 3 / 4
	tail := maximum - head
	return string(runes[:head]) + "\n…\n" + string(runes[len(runes)-tail:])
}
