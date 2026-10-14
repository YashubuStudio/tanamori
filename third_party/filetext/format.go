package filetext

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// Format identifies the parser used for a file.
type Format string

const (
	FormatAuto   Format = "auto"
	FormatText   Format = "text"
	FormatHTML   Format = "html"
	FormatXML    Format = "xml"
	FormatJSON   Format = "json"
	FormatJSONL  Format = "jsonl"
	FormatYAML   Format = "yaml"
	FormatTOML   Format = "toml"
	FormatINI    Format = "ini"
	FormatCSV    Format = "csv"
	FormatTSV    Format = "tsv"
	FormatDOCX   Format = "docx"
	FormatPPTX   Format = "pptx"
	FormatXLSX   Format = "xlsx"
	FormatODF    Format = "odf"
	FormatPDF    Format = "pdf"
	FormatRTF    Format = "rtf"
	FormatEPUB   Format = "epub"
	FormatSRT    Format = "srt"
	FormatVTT    Format = "vtt"
	FormatTeX    Format = "tex"
	FormatBibTeX Format = "bibtex"
	FormatEmail  Format = "email"
	FormatZIP    Format = "zip"
	FormatTAR    Format = "tar"
	FormatGZIP   Format = "gzip"
	FormatBZIP2  Format = "bzip2"
	FormatBinary Format = "binary-strings"
)

var extensionFormats = map[string]Format{
	".html": FormatHTML, ".htm": FormatHTML, ".xhtml": FormatHTML,
	".xml": FormatXML, ".svg": FormatXML, ".rss": FormatXML, ".atom": FormatXML,
	".kml": FormatXML, ".gpx": FormatXML, ".xsd": FormatXML, ".xsl": FormatXML,
	".xslt": FormatXML, ".plist": FormatXML, ".opf": FormatXML, ".ncx": FormatXML,
	".fb2": FormatXML, ".xlf": FormatXML, ".xliff": FormatXML,
	".fodt": FormatXML, ".fods": FormatXML, ".fodp": FormatXML,
	".json": FormatJSON, ".jsonl": FormatJSONL, ".ndjson": FormatJSONL,
	".yaml": FormatYAML, ".yml": FormatYAML, ".toml": FormatTOML,
	".ini": FormatINI, ".csv": FormatCSV, ".tsv": FormatTSV,
	".docx": FormatDOCX, ".dotx": FormatDOCX, ".docm": FormatDOCX, ".dotm": FormatDOCX,
	".pptx": FormatPPTX, ".pptm": FormatPPTX, ".potx": FormatPPTX, ".potm": FormatPPTX,
	".ppsx": FormatPPTX, ".ppsm": FormatPPTX,
	".xlsx": FormatXLSX, ".xlsm": FormatXLSX, ".xltx": FormatXLSX, ".xltm": FormatXLSX,
	".odt": FormatODF, ".ott": FormatODF, ".ods": FormatODF, ".ots": FormatODF,
	".odp": FormatODF, ".otp": FormatODF,
	".pdf": FormatPDF, ".rtf": FormatRTF, ".epub": FormatEPUB,
	".srt": FormatSRT, ".vtt": FormatVTT, ".tex": FormatTeX,
	".bib": FormatBibTeX, ".eml": FormatEmail, ".mht": FormatEmail, ".mhtml": FormatEmail,
	".zip": FormatZIP, ".jar": FormatZIP, ".war": FormatZIP, ".ear": FormatZIP,
	".whl": FormatZIP, ".apk": FormatZIP, ".aab": FormatZIP, ".ipa": FormatZIP,
	".nupkg": FormatZIP, ".vsix": FormatZIP, ".cbz": FormatZIP, ".kmz": FormatZIP,
	".tar": FormatTAR, ".tar.gz": FormatTAR, ".tar.bz2": FormatTAR,
	".tgz": FormatTAR, ".tbz": FormatTAR, ".tbz2": FormatTAR,
	".gz": FormatGZIP, ".gzip": FormatGZIP, ".bz2": FormatBZIP2, ".bzip2": FormatBZIP2,
	".doc": FormatBinary, ".xls": FormatBinary, ".ppt": FormatBinary, ".msg": FormatBinary,
	".mobi": FormatBinary, ".azw": FormatBinary, ".azw3": FormatBinary, ".chm": FormatBinary,
}

var textExtensions = map[string]struct{}{
	".txt": {}, ".text": {}, ".md": {}, ".markdown": {}, ".mdown": {}, ".mkd": {},
	".rst": {}, ".adoc": {}, ".asciidoc": {}, ".org": {}, ".wiki": {},
	".log": {}, ".out": {}, ".diff": {}, ".patch": {},
	".go": {}, ".mod": {}, ".sum": {}, ".work": {},
	".py": {}, ".pyi": {}, ".js": {}, ".mjs": {}, ".cjs": {}, ".jsx": {},
	".ts": {}, ".tsx": {}, ".java": {}, ".kt": {}, ".kts": {}, ".scala": {},
	".groovy": {}, ".c": {}, ".h": {}, ".cc": {}, ".cpp": {}, ".cxx": {},
	".hh": {}, ".hpp": {}, ".cs": {}, ".fs": {}, ".fsx": {}, ".vb": {},
	".rs": {}, ".swift": {}, ".m": {}, ".mm": {}, ".php": {}, ".rb": {},
	".pl": {}, ".pm": {}, ".lua": {}, ".r": {}, ".dart": {}, ".ex": {},
	".exs": {}, ".erl": {}, ".hrl": {}, ".clj": {}, ".cljs": {}, ".cljc": {},
	".edn": {}, ".hs": {}, ".lhs": {}, ".ml": {}, ".mli": {}, ".asm": {},
	".s": {}, ".sql": {}, ".graphql": {}, ".gql": {}, ".proto": {},
	".thrift": {}, ".sol": {}, ".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {},
	".ps1": {}, ".bat": {}, ".cmd": {}, ".css": {}, ".scss": {}, ".sass": {},
	".less": {}, ".vue": {}, ".svelte": {}, ".conf": {}, ".config": {},
	".cfg": {}, ".env": {}, ".properties": {}, ".editorconfig": {},
	".gitignore": {}, ".gitattributes": {}, ".dockerignore": {}, ".npmrc": {},
	".ics": {}, ".vcf": {}, ".desktop": {}, ".url": {}, ".reg": {},
	".manifest": {}, ".mf": {}, ".gradle": {}, ".cmake": {}, ".mak": {},
	".mk": {}, ".lock": {}, ".po": {}, ".pot": {},
}

var textBaseNames = map[string]struct{}{
	"dockerfile": {}, "makefile": {}, "gnumakefile": {}, "cmakelists.txt": {},
	"license": {}, "licence": {}, "readme": {}, "changelog": {}, "authors": {},
	"contributors": {}, "notice": {}, "procfile": {}, "gemfile": {}, "rakefile": {},
	"vagrantfile": {}, "jenkinsfile": {},
}

// NormalizeFormat validates a format name or extension.
func NormalizeFormat(value string) (Format, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == string(FormatAuto) {
		return FormatAuto, nil
	}
	if !strings.HasPrefix(value, ".") {
		candidate := Format(value)
		for _, format := range allFormats() {
			if candidate == format {
				return candidate, nil
			}
		}
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	if format, ok := extensionFormats[value]; ok {
		return format, nil
	}
	if _, ok := textExtensions[value]; ok {
		return FormatText, nil
	}
	return "", fmt.Errorf("filetext: unknown format %q", value)
}

// DetectFormat detects a parser using strong file signatures, ZIP contents,
// the file name, and finally a binary/text heuristic.
func DetectFormat(path string) (Format, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	header := make([]byte, 8192)
	n, readErr := file.Read(header)
	file.Close()
	if readErr != nil && readErr != io.EOF {
		return "", readErr
	}
	header = header[:n]

	switch {
	case bytes.HasPrefix(header, []byte("%PDF-")):
		return FormatPDF, nil
	case bytes.HasPrefix(header, []byte{'P', 'K', 3, 4}), bytes.HasPrefix(header, []byte{'P', 'K', 5, 6}):
		return detectZIPFormat(path)
	case bytes.HasPrefix(header, []byte{0x1f, 0x8b}):
		if strings.HasSuffix(strings.ToLower(path), ".tar.gz") || strings.HasSuffix(strings.ToLower(path), ".tgz") {
			return FormatTAR, nil
		}
		return FormatGZIP, nil
	case bytes.HasPrefix(header, []byte("BZh")):
		lowerPath := strings.ToLower(path)
		if strings.HasSuffix(lowerPath, ".tar.bz2") || strings.HasSuffix(lowerPath, ".tbz") || strings.HasSuffix(lowerPath, ".tbz2") {
			return FormatTAR, nil
		}
		return FormatBZIP2, nil
	case bytes.HasPrefix(header, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}):
		return FormatBinary, nil
	case bytes.HasPrefix(bytes.TrimSpace(header), []byte("{\\rtf")):
		return FormatRTF, nil
	}

	if format, ok := formatFromPath(path); ok {
		return format, nil
	}
	trimmed := bytes.TrimSpace(header)
	lower := bytes.ToLower(trimmed)
	if looksLikeEmailHeader(header) {
		return FormatEmail, nil
	}
	if bytes.HasPrefix(lower, []byte("<!doctype html")) || bytes.HasPrefix(lower, []byte("<html")) {
		return FormatHTML, nil
	}
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		return FormatXML, nil
	}
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return FormatJSON, nil
	}
	if isLikelyText(header) {
		return FormatText, nil
	}
	return FormatBinary, nil
}

func formatFromPath(path string) (Format, bool) {
	lowerPath := strings.ToLower(path)
	if strings.HasSuffix(lowerPath, ".tar.gz") || strings.HasSuffix(lowerPath, ".tar.bz2") {
		return FormatTAR, true
	}
	base := strings.ToLower(filepath.Base(path))
	if _, ok := textBaseNames[base]; ok {
		return FormatText, true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if format, ok := extensionFormats[ext]; ok {
		return format, true
	}
	_, ok := textExtensions[ext]
	return FormatText, ok
}

func detectZIPFormat(path string) (Format, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	if len(reader.File) > maxArchiveEntries {
		return "", fmt.Errorf("filetext: ZIP has too many entries: %d", len(reader.File))
	}
	seen := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		seen[strings.ToLower(file.Name)] = true
	}
	switch {
	case seen["word/document.xml"]:
		return FormatDOCX, nil
	case seen["ppt/presentation.xml"]:
		return FormatPPTX, nil
	case seen["xl/workbook.xml"]:
		return FormatXLSX, nil
	case seen["meta-inf/container.xml"]:
		return FormatEPUB, nil
	case seen["content.xml"]:
		return FormatODF, nil
	default:
		return FormatZIP, nil
	}
}

func isLikelyText(sample []byte) bool {
	if len(sample) == 0 {
		return true
	}
	if bytes.HasPrefix(sample, []byte{0xff, 0xfe}) || bytes.HasPrefix(sample, []byte{0xfe, 0xff}) || looksLikeUTF16(sample) {
		return true
	}
	if bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	return utf8.Valid(sample) || DetectEncoding(sample) != nil
}

func looksLikeUTF16(sample []byte) bool {
	if len(sample) < 8 {
		return false
	}
	var evenNUL, oddNUL int
	for index, value := range sample {
		if value != 0 {
			continue
		}
		if index%2 == 0 {
			evenNUL++
		} else {
			oddNUL++
		}
	}
	pairs := len(sample) / 2
	return evenNUL > pairs/2 || oddNUL > pairs/2
}

func looksLikeEmailHeader(sample []byte) bool {
	if len(sample) == 0 || bytes.IndexByte(sample, 0) >= 0 {
		return false
	}
	lower := strings.ToLower(string(sample))
	separator := strings.Index(lower, "\n\n")
	if crlf := strings.Index(lower, "\r\n\r\n"); crlf >= 0 && (separator < 0 || crlf < separator) {
		separator = crlf
	}
	if separator >= 0 {
		lower = lower[:separator]
	}
	return (strings.HasPrefix(lower, "from:") || strings.Contains(lower, "\nfrom:")) &&
		(strings.Contains(lower, "\nsubject:") || strings.Contains(lower, "\nmime-version:") || strings.Contains(lower, "\ncontent-type:"))
}

// SupportedExtensions returns recognized extensions in stable order.
func SupportedExtensions() []string {
	extensions := make([]string, 0, len(extensionFormats)+len(textExtensions))
	for ext := range extensionFormats {
		extensions = append(extensions, ext)
	}
	for ext := range textExtensions {
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions
}

func allFormats() []Format {
	return []Format{FormatAuto, FormatText, FormatHTML, FormatXML, FormatJSON, FormatJSONL,
		FormatYAML, FormatTOML, FormatINI, FormatCSV, FormatTSV, FormatDOCX, FormatPPTX,
		FormatXLSX, FormatODF, FormatPDF, FormatRTF, FormatEPUB, FormatSRT, FormatVTT,
		FormatTeX, FormatBibTeX, FormatEmail, FormatZIP, FormatTAR, FormatGZIP, FormatBZIP2, FormatBinary}
}
