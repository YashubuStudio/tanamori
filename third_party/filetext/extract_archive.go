package filetext

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/transform"
)

const (
	maxArchiveEntries    = 4096
	maxArchiveEntryBytes = uint64(128 << 20)
	maxArchiveTotalBytes = uint64(512 << 20)
)

// ExtractTextFromZIP extracts readable members without writing them to disk.
func ExtractTextFromZIP(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) > maxArchiveEntries {
		return "", fmt.Errorf("filetext: ZIP has too many entries: %d", len(archive.File))
	}
	var output strings.Builder
	var total uint64
	for _, file := range archive.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxArchiveEntryBytes {
			return "", fmt.Errorf("filetext: ZIP entry %q exceeds %d bytes", file.Name, maxArchiveEntryBytes)
		}
		total += file.UncompressedSize64
		if total > maxArchiveTotalBytes {
			return "", fmt.Errorf("filetext: ZIP expanded data exceeds %d bytes", maxArchiveTotalBytes)
		}
		reader, err := file.Open()
		if err != nil {
			return "", err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, int64(maxArchiveEntryBytes)+1))
		closeErr := reader.Close()
		if readErr != nil {
			return "", readErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		if uint64(len(data)) > maxArchiveEntryBytes {
			return "", fmt.Errorf("filetext: ZIP entry %q exceeds %d bytes", file.Name, maxArchiveEntryBytes)
		}
		text, ok, err := extractArchiveMember(file.Name, data)
		if err != nil {
			return "", err
		}
		if ok {
			appendArchiveSection(&output, file.Name, text)
		}
	}
	return output.String(), nil
}

// ExtractTextFromTAR extracts readable members from TAR or compressed TAR.
func ExtractTextFromTAR(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var reader io.Reader = file
	magic := make([]byte, 3)
	n, _ := file.Read(magic)
	file.Seek(0, io.SeekStart)
	if n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return "", err
		}
		defer compressed.Close()
		reader = compressed
	} else if n == 3 && string(magic) == "BZh" {
		reader = bzip2.NewReader(file)
	}
	return extractTARReader(reader)
}

// ExtractTextFromBZIP2 extracts a single BZIP2-compressed text member. TAR
// streams are traversed without expanding them to disk.
func ExtractTextFromBZIP2(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	compressed := bufio.NewReader(bzip2.NewReader(file))
	peek, _ := compressed.Peek(512)
	lowerPath := strings.ToLower(path)
	if strings.HasSuffix(lowerPath, ".tar.bz2") || strings.HasSuffix(lowerPath, ".tbz") || strings.HasSuffix(lowerPath, ".tbz2") || looksLikeTAR(peek) {
		return extractTARReader(compressed)
	}
	data, err := io.ReadAll(io.LimitReader(compressed, int64(maxArchiveEntryBytes)+1))
	if err != nil {
		return "", err
	}
	if uint64(len(data)) > maxArchiveEntryBytes {
		return "", fmt.Errorf("filetext: BZIP2 output exceeds %d bytes", maxArchiveEntryBytes)
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	text, ok, err := extractArchiveMember(name, data)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("filetext: compressed member %q does not contain readable text", name)
	}
	return text, nil
}

func looksLikeTAR(header []byte) bool {
	return len(header) >= 265 && (string(header[257:263]) == "ustar\x00" || string(header[257:265]) == "ustar  \x00")
}

// ExtractTextFromGZIP extracts a single compressed text member. If the member
// is a TAR archive it is traversed with the same safety limits as TAR input.
func ExtractTextFromGZIP(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return "", err
	}
	defer compressed.Close()
	if strings.HasSuffix(strings.ToLower(compressed.Name), ".tar") || strings.HasSuffix(strings.ToLower(path), ".tar.gz") {
		return extractTARReader(compressed)
	}
	data, err := io.ReadAll(io.LimitReader(compressed, int64(maxArchiveEntryBytes)+1))
	if err != nil {
		return "", err
	}
	if uint64(len(data)) > maxArchiveEntryBytes {
		return "", fmt.Errorf("filetext: GZIP output exceeds %d bytes", maxArchiveEntryBytes)
	}
	name := compressed.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	text, ok, err := extractArchiveMember(name, data)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("filetext: compressed member %q does not contain readable text", name)
	}
	return text, nil
}

func extractTARReader(reader io.Reader) (string, error) {
	archive := tar.NewReader(reader)
	var output strings.Builder
	var total uint64
	entries := 0
	for {
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeRegA {
			continue
		}
		entries++
		if entries > maxArchiveEntries {
			return "", fmt.Errorf("filetext: TAR has more than %d file entries", maxArchiveEntries)
		}
		if header.Size < 0 || uint64(header.Size) > maxArchiveEntryBytes {
			return "", fmt.Errorf("filetext: TAR entry %q is too large", header.Name)
		}
		total += uint64(header.Size)
		if total > maxArchiveTotalBytes {
			return "", fmt.Errorf("filetext: TAR expanded data exceeds %d bytes", maxArchiveTotalBytes)
		}
		data, err := io.ReadAll(io.LimitReader(archive, header.Size))
		if err != nil {
			return "", err
		}
		text, ok, err := extractArchiveMember(header.Name, data)
		if err != nil {
			return "", err
		}
		if ok {
			appendArchiveSection(&output, header.Name, text)
		}
	}
	return output.String(), nil
}

func extractArchiveMember(name string, data []byte) (string, bool, error) {
	format, known := formatFromPath(name)
	if !known {
		if !isLikelyText(data) {
			return "", false, nil
		}
		format = FormatText
	}
	switch format {
	case FormatText, FormatCSV, FormatTSV, FormatJSONL, FormatYAML, FormatTOML, FormatINI,
		FormatSRT, FormatVTT, FormatTeX, FormatBibTeX:
		decoded, err := decodeTextBytes(data)
		return decoded, err == nil, err
	case FormatHTML:
		decoded, err := decodeTextBytes(data)
		if err != nil {
			return "", false, err
		}
		return stripHTMLTags(decoded), true, nil
	case FormatXML:
		decoded, err := decodeTextBytes(data)
		if err != nil {
			return "", false, err
		}
		text, err := extractXMLText(strings.NewReader(decoded))
		return text, err == nil, err
	case FormatJSON:
		decoded, err := decodeTextBytes(data)
		if err != nil {
			return "", false, err
		}
		return decoded, true, nil
	default:
		return "", false, nil
	}
}

func decodeTextBytes(data []byte) (string, error) {
	encoding := DetectEncoding(data)
	if encoding == nil {
		return "", fmt.Errorf("filetext: could not detect text encoding")
	}
	decoded, _, err := transform.Bytes(encoding.NewDecoder(), data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func appendArchiveSection(output *strings.Builder, name, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if output.Len() > 0 {
		output.WriteString("\n\n")
	}
	output.WriteString("===== ")
	output.WriteString(filepath.ToSlash(name))
	output.WriteString(" =====\n")
	output.WriteString(text)
}
