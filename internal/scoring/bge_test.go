package scoring

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/app"
)

func TestClipTextKeepsHeadAndTail(t *testing.T) {
	value := "先頭" + strings.Repeat("中", 20) + "末尾"
	clipped := clipText(value, 12)
	if !strings.HasPrefix(clipped, "先頭") || !strings.HasSuffix(clipped, "末尾") {
		t.Fatalf("head or tail was lost: %q", clipped)
	}
	if !strings.Contains(clipped, "…") {
		t.Fatalf("omission marker is missing: %q", clipped)
	}
}

func TestNewBGERerankerRequiresPaths(t *testing.T) {
	if _, err := NewBGEReranker(BGEConfig{}); err == nil {
		t.Fatal("empty configuration must fail")
	}
}
func TestTailBufferKeepsBoundedSuffix(t *testing.T) {
	var buffer tailBuffer
	prefix := strings.Repeat("a", 40*1024)
	if count, err := buffer.Write([]byte(prefix + "tail")); err != nil || count != len(prefix)+4 {
		t.Fatalf("Write() = %d, %v", count, err)
	}
	if got := buffer.String(); len(got) != 32*1024 || !strings.HasSuffix(got, "tail") {
		t.Fatalf("unexpected bounded buffer: len=%d suffix=%t", len(got), strings.HasSuffix(got, "tail"))
	}
	buffer.Reset()
	if got := buffer.String(); got != "" {
		t.Fatalf("Reset() left %d bytes", len(got))
	}
}
func TestBGERerankerWorkerProtocol(t *testing.T) {
	temporary := t.TempDir()
	runner := filepath.Join(temporary, "runtime.py")
	if err := os.WriteFile(runner, []byte("test runner placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	modelDir := filepath.Join(temporary, "model")
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	reranker, err := NewBGEReranker(BGEConfig{
		ModelID: "test-model", Revision: "test-revision", ModelDir: modelDir,
		Python: os.Args[0], Runner: runner,
		InterpreterArgs: []string{"-test.run=^TestBGEWorkerHelperProcess$", "--"},
		Environment:     []string{"TANAMORI_BGE_HELPER=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reranker.Close()
	request := app.ScoreRequest{LeftText: "file passage", RightText: "folder query"}
	first, err := reranker.Score(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reranker.Score(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Score != 0.25 || second.Score != 0.50 {
		t.Fatalf("worker was not reused: first=%v second=%v", first.Score, second.Score)
	}
	if first.ModelVersion != "test-model@test-revision" {
		t.Fatalf("model version = %q", first.ModelVersion)
	}
}

func TestBGEWorkerHelperProcess(t *testing.T) {
	if os.Getenv("TANAMORI_BGE_HELPER") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(workerMessage{Type: "ready", ModelID: "test-model", Revision: "test-revision", Device: "test"})
	scanner := bufio.NewScanner(os.Stdin)
	count := 0
	for scanner.Scan() {
		var request workerRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			_ = encoder.Encode(workerMessage{ID: request.ID, Error: err.Error()})
			continue
		}
		if request.Query != "file passage" || request.Passage != "folder query" {
			_ = encoder.Encode(workerMessage{ID: request.ID, Error: "query/passage order is invalid"})
			continue
		}
		count++
		_ = encoder.Encode(workerMessage{ID: request.ID, Score: float64(count) * 0.25, DurationMS: 1})
	}
	os.Exit(0)
}
