package filetext

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectLineRanges(t *testing.T) {
	got := selectForTest(t, "one\ntwo\nthree\nfour\nfive\n", Selection{Lines: "2-3,5"})
	if want := "two\nthree\nfive\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectRegexpWithContext(t *testing.T) {
	input := "before\nERROR failure\nafter\nunrelated\n"
	got := selectForTest(t, input, Selection{Match: "^error", IgnoreCase: true, Before: 1, After: 1})
	if want := "before\nERROR failure\nafter\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectFromToAndTail(t *testing.T) {
	input := "skip\nSTART\na\nb\nEND\nskip too\n"
	got := selectForTest(t, input, Selection{From: "start", To: "end", IgnoreCase: true, Tail: 2})
	if want := "b\nEND\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectRuneLimitDoesNotSplitUTF8(t *testing.T) {
	got := selectForTest(t, "猫abc\n", Selection{MaxChars: 2})
	if want := "猫a"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadUnknownExtensionAsText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.custom")
	if err := os.WriteFile(path, []byte("plain text inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != "plain text inside" {
		t.Fatalf("got %q", got)
	}
}

func TestHeadAndTailAreMutuallyExclusive(t *testing.T) {
	var output strings.Builder
	err := writeSelected(context.Background(), strings.NewReader("a\n"), &output, Selection{Head: 1, Tail: 1})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func selectForTest(t *testing.T, input string, selection Selection) string {
	t.Helper()
	var output bytes.Buffer
	if err := writeSelected(context.Background(), strings.NewReader(input), &output, selection); err != nil {
		t.Fatal(err)
	}
	return output.String()
}
