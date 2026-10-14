package filetext

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractStructuredPaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.json")
	if err := os.WriteFile(path, []byte(`{"user":{"name":"Aki"},"tags":["go","ai"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "tags[0]: go\ntags[1]: ai\nuser.name: Aki\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractMinimalDOCX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	document, err := archive.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = document.Write([]byte(`<w:document xmlns:w="urn:test"><w:body><w:p><w:r><w:t>Hello</w:t></w:r></w:p><w:p><w:r><w:t>世界</w:t></w:r></w:p></w:body></w:document>`))
	if err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Hello\n世界") {
		t.Fatalf("unexpected DOCX text: %q", got)
	}
}
