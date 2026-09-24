package modelsetup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManifestAndUninstalledStatus(t *testing.T) {
	installer, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if installer.Manifest.ModelID != "BAAI/bge-reranker-v2-m3" {
		t.Fatalf("model ID = %q", installer.Manifest.ModelID)
	}
	if installer.Manifest.WeightSize != 2271071852 {
		t.Fatalf("weight size = %d", installer.Manifest.WeightSize)
	}
	status, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready {
		t.Fatal("fresh model root must not be ready")
	}
	if status.Paths.ModelDir == "" || status.RuntimePython == "" {
		t.Fatalf("status paths are incomplete: %+v", status.Paths)
	}
}

func TestStatusRejectsTruncatedWeight(t *testing.T) {
	installer, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{installer.Paths.ModelDir, installer.Paths.RuntimeDir, filepath.Dir(installer.Paths.Python)} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{installer.Paths.Python, installer.Paths.Runner, installer.Paths.Weight} {
		if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := `{"schema_version":1,"model_id":"BAAI/bge-reranker-v2-m3","revision":"953dc6f6f85a1b2dbfca4c34a2796e7dde08d41e","weight_sha256":"d9e3e081faff1eefb84019509b2f5558fd74c1a05a2c7db22f74174fcedb5286","installed_at":"2026-01-01T00:00:00Z","python_version":"3.10.0"}`
	if err := os.WriteFile(installer.Paths.Marker, []byte(marker), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err := installer.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.Ready {
		t.Fatal("truncated weight must not be ready")
	}
}
