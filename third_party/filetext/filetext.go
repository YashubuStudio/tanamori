package filetext

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
)

// Options controls format detection and optional text selection.
type Options struct {
	// Format overrides content and extension detection. Empty and "auto" detect.
	Format    Format
	Selection Selection
}

// Read returns all extractable text from path.
func Read(path string) (string, error) {
	return ReadWithOptions(context.Background(), path, Options{})
}

// ReadAs returns all extractable text using an explicit parser.
func ReadAs(path string, format Format) (string, error) {
	return ReadWithOptions(context.Background(), path, Options{Format: format})
}

// ReadWithOptions extracts text and applies the requested selection.
func ReadWithOptions(ctx context.Context, path string, opts Options) (string, error) {
	var output bytes.Buffer
	if err := Write(ctx, path, &output, opts); err != nil {
		return "", err
	}
	return output.String(), nil
}

// Write extracts text from path into dst. Plain text inputs are decoded and
// filtered as a stream, so selections do not require loading the complete file
// into memory. Container formats use their format parser first.
func Write(ctx context.Context, path string, dst io.Writer, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if dst == nil {
		return fmt.Errorf("filetext: destination writer is nil")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	format, err := NormalizeFormat(string(opts.Format))
	if err != nil {
		return err
	}
	if format == FormatAuto {
		format, err = DetectFormat(path)
		if err != nil {
			return err
		}
	}
	if format == FormatText {
		reader, closeFn, err := openDecodedText(path)
		if err != nil {
			return err
		}
		defer closeFn()
		return writeSelected(ctx, reader, dst, opts.Selection)
	}

	text, err := readAllTextAs(path, format)
	if err != nil {
		return err
	}
	return writeSelected(ctx, strings.NewReader(text), dst, opts.Selection)
}
