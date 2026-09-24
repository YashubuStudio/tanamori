package bgereranker

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRuntimeProtocolIsUTF8(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		t.Skip("Python is not available")
	}
	runner := filepath.Join(t.TempDir(), "runtime.py")
	if err := os.WriteFile(runner, Runtime(), 0o600); err != nil {
		t.Fatal(err)
	}
	const want = "日本語の契約書と絵文字📄"
	request, err := json.Marshal(map[string]string{"text": want})
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(python, runner, "protocol-check")
	command.Stdin = bytes.NewReader(append(request, '\n'))
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("protocol check: %v: %s", err, stderr.String())
	}
	var response struct {
		Text   string `json:"text"`
		Length int    `json:"length"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		t.Fatalf("decode response %q: %v", output, err)
	}
	if response.Text != want || response.Length != len([]rune(want)) {
		t.Fatalf("UTF-8 round trip = %#v", response)
	}
}
