package filetext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTextFileDoesNotDuplicateDetectionSample(t *testing.T) {
	want := strings.Repeat("a", 1200) + "終端"
	path := filepath.Join(t.TempDir(), "sample.txt")
	if err := os.WriteFile(path, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadTextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("content changed while decoding: got %d bytes, want %d", len(got), len(want))
	}
}

func TestReadTextFileHandlesUTF8RuneAcrossSampleBoundary(t *testing.T) {
	want := strings.Repeat("a", 1023) + "猫tail"
	path := filepath.Join(t.TempDir(), "boundary.txt")
	if err := os.WriteFile(path, []byte(want), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("UTF-8 boundary was decoded incorrectly: got %d bytes, want %d", len(got), len(want))
	}
}

func TestReadTextFileStripsUTF8BOM(t *testing.T) {
	want := "visible text"
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(want)...)
	path := filepath.Join(t.TempDir(), "bom.txt")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadTextFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("UTF-8 BOM was not removed: %q", got)
	}
}

func TestStripHTMLTagsKeepsOnlyVisibleText(t *testing.T) {
	input := `<html><head><style>.secret{display:none}</style><script>hiddenCall()</script></head><body><h1>Visible</h1><p>text</p></body></html>`
	got := stripHTMLTags(input)
	if !strings.Contains(got, "Visible") || !strings.Contains(got, "text") {
		t.Fatalf("visible text missing: %q", got)
	}
	for _, hidden := range []string{"secret", "hiddenCall"} {
		if strings.Contains(got, hidden) {
			t.Fatalf("non-visible content %q was extracted: %q", hidden, got)
		}
	}
}

func TestIsNumericRequiresWholeLine(t *testing.T) {
	if !isNumeric(" 42 ") {
		t.Fatal("expected a numeric cue line")
	}
	if isNumeric("42nd Street") {
		t.Fatal("dialogue beginning with a number must not be treated as a cue")
	}
}

func TestIsSupportedIsCaseInsensitive(t *testing.T) {
	if !IsSupported("BOOK.XLSX") {
		t.Fatal("expected XLSX to be supported")
	}
	if !IsSupported("archive.zip") {
		t.Fatal("expected ZIP to be supported")
	}
}
