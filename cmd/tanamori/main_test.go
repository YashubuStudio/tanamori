package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommand(t *testing.T) {
	database := filepath.Join(t.TempDir(), "library.db")
	var stdout, stderr bytes.Buffer
	code := run([]string{"init", "--db", database}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "ready"`) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestMissingCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d", code)
	}
}
func TestModelStatusDoesNotDownload(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"model-status", "--model-root", t.TempDir()}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ready": false`) {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}
func TestModelVerifyFailsWhenUninstalled(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"model-verify", "--model-root", t.TempDir()}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "model is not installed") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
