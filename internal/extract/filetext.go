package extract

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/YashubuStudio/filetext"
	"github.com/YashubuStudio/tanamori/internal/domain"
)

const defaultFiletextVersion = "workspace-0.2"

type Adapter struct {
	MaxChars         int
	MaxSourceBytes   int64
	MaxExpandedBytes uint64
	ChunkChars       int
	OverlapChars     int
	FiletextVersion  string
}

func DefaultAdapter() Adapter {
	return Adapter{
		MaxChars: 2_000_000, MaxSourceBytes: 64 << 20, MaxExpandedBytes: 64 << 20,
		ChunkChars: 8_000, OverlapChars: 200,
		FiletextVersion: defaultFiletextVersion,
	}
}

func (adapter Adapter) Extract(ctx context.Context, path string) (domain.Extraction, error) {
	if adapter.MaxChars <= 0 {
		return domain.Extraction{}, fmt.Errorf("max characters must be positive")
	}
	if adapter.ChunkChars <= 0 || adapter.OverlapChars < 0 || adapter.OverlapChars >= adapter.ChunkChars {
		return domain.Extraction{}, fmt.Errorf("invalid chunk configuration")
	}
	format, err := filetext.DetectFormat(path)
	if err != nil {
		return domain.Extraction{}, fmt.Errorf("detect format: %w", err)
	}
	if err := adapter.checkInput(path, format); err != nil {
		return domain.Extraction{}, err
	}
	var output strings.Builder
	err = filetext.Write(ctx, path, &output, filetext.Options{
		Format:    format,
		Selection: filetext.Selection{MaxChars: adapter.MaxChars},
	})
	if err != nil {
		return domain.Extraction{}, fmt.Errorf("extract %s: %w", format, err)
	}
	text := output.String()
	return domain.Extraction{
		Format: string(format), Chunks: Chunk(text, adapter.ChunkChars, adapter.OverlapChars),
		FiletextVersion: adapter.FiletextVersion,
		Truncated:       utf8.RuneCountInString(text) >= adapter.MaxChars,
	}, nil
}

func (adapter Adapter) checkInput(path string, format filetext.Format) error {
	if format == filetext.FormatText {
		return nil
	} // streamed by filetext.Write
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat extraction source: %w", err)
	}
	if adapter.MaxSourceBytes > 0 && info.Size() > adapter.MaxSourceBytes {
		return fmt.Errorf("non-text source exceeds %d byte parser limit", adapter.MaxSourceBytes)
	}
	if adapter.MaxExpandedBytes == 0 {
		return nil
	}
	switch format {
	case filetext.FormatZIP, filetext.FormatDOCX, filetext.FormatPPTX,
		filetext.FormatXLSX, filetext.FormatODF, filetext.FormatEPUB:
	default:
		return nil
	}
	archive, err := zip.OpenReader(path)
	if err != nil {
		return fmt.Errorf("inspect ZIP members: %w", err)
	}
	defer archive.Close()
	var total uint64
	for _, member := range archive.File {
		if member.UncompressedSize64 > adapter.MaxExpandedBytes-total {
			return fmt.Errorf("ZIP content exceeds %d byte parser limit", adapter.MaxExpandedBytes)
		}
		total += member.UncompressedSize64
	}
	return nil
}

func Chunk(text string, size, overlap int) []string {
	if size <= 0 || overlap < 0 || overlap >= size {
		return nil
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return nil
	}
	chunks := make([]string, 0, (len(runes)+size-1)/size)
	step := size - overlap
	for start := 0; start < len(runes); start += step {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
	}
	return chunks
}
