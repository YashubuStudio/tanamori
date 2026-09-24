package extract

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAdapterRejectsOversizedZIPBeforeExtraction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.Create("inside.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("a hundred characters of content to trigger the expanded archive limit")); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	adapter := DefaultAdapter()
	adapter.MaxExpandedBytes = 16
	if _, err := adapter.Extract(context.Background(), path); err == nil {
		t.Fatal("oversized expanded ZIP was accepted")
	}
}

func TestChunkKeepsUnicodeAndOverlap(t *testing.T) {
	got := Chunk("あいうえおかきくけこ", 6, 2)
	if len(got) != 2 || got[0] != "あいうえおか" || got[1] != "おかきくけこ" {
		t.Fatalf("unexpected chunks: %#v", got)
	}
}

func TestAdapterExtractsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte("TANAMORI searchable text"), 0o600); err != nil {
		t.Fatal(err)
	}
	adapter := DefaultAdapter()
	result, err := adapter.Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Format != "text" || len(result.Chunks) != 1 || result.Chunks[0] != "TANAMORI searchable text" {
		t.Fatalf("unexpected extraction: %#v", result)
	}
}
