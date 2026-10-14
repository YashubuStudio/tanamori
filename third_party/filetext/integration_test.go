package filetext

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/base64"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestDetectExtensionlessDOCX(t *testing.T) {
	path := filepath.Join(t.TempDir(), "document.bin")
	writeZIP(t, path, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   `<w:document xmlns:w="urn:test"><w:body><w:p><w:r><w:t>Detected</w:t></w:r></w:p></w:body></w:document>`,
	})
	format, err := DetectFormat(path)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatDOCX {
		t.Fatalf("got format %q", format)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Detected") {
		t.Fatalf("unexpected text %q", text)
	}
}

func TestExtractOpenDocumentSpreadsheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sheet.ods")
	writeZIP(t, path, map[string]string{
		"mimetype":    "application/vnd.oasis.opendocument.spreadsheet",
		"content.xml": `<office:document-content xmlns:office="urn:o" xmlns:text="urn:t" xmlns:table="urn:table"><office:body><table:table><table:table-row><table:table-cell><text:p>Alpha</text:p></table:table-cell><table:table-cell><text:p>Beta</text:p></table:table-cell></table:table-row></table:table></office:body></office:document-content>`,
	})
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Alpha") || !strings.Contains(text, "Beta") {
		t.Fatalf("unexpected ODS text %q", text)
	}
}

func TestStructuredFilesDecodeUTF16(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "data.json")
	if err := os.WriteFile(jsonPath, encodeUTF16LE(`{"name":"世界"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := Read(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "name: 世界") {
		t.Fatalf("unexpected UTF-16 JSON text %q", text)
	}

	xmlPath := filepath.Join(dir, "data.xml")
	if err := os.WriteFile(xmlPath, encodeUTF16LE(`<root><name>世界</name></root>`), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err = Read(xmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if text != "世界" {
		t.Fatalf("unexpected UTF-16 XML text %q", text)
	}
}
func TestExtractJSONLinesAndTSV(t *testing.T) {
	dir := t.TempDir()
	jsonl := filepath.Join(dir, "events.ndjson")
	if err := os.WriteFile(jsonl, []byte("{\"event\":\"start\"}\n{\"event\":\"stop\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := Read(jsonl)
	if err != nil {
		t.Fatal(err)
	}
	if text != "[1].event: start\n[2].event: stop\n" {
		t.Fatalf("unexpected JSONL text %q", text)
	}
	tsv := filepath.Join(dir, "table.tsv")
	if err := os.WriteFile(tsv, []byte("name\tvalue\nA\t1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err = Read(tsv)
	if err != nil {
		t.Fatal(err)
	}
	if text != "name\tvalue\nA\t1" {
		t.Fatalf("unexpected TSV text %q", text)
	}
}

func TestExtractZIPAndGZIP(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "bundle.zip")
	writeZIP(t, zipPath, map[string]string{"notes/readme.txt": "zip text", "binary.dat": "\x00\x01"})
	text, err := Read(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "notes/readme.txt") || !strings.Contains(text, "zip text") {
		t.Fatalf("unexpected ZIP text %q", text)
	}

	gzipPath := filepath.Join(dir, "notes.txt.gz")
	file, err := os.Create(gzipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := gzip.NewWriter(file)
	writer.Name = "notes.txt"
	if _, err := writer.Write([]byte("gzip text")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	text, err = Read(gzipPath)
	if err != nil {
		t.Fatal(err)
	}
	if text != "gzip text" {
		t.Fatalf("unexpected GZIP text %q", text)
	}
}

func TestExtractBZIP2(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt.bz2")
	fixture, err := base64.StdEncoding.DecodeString("QlpoOTFBWSZTWV9Vq7EAAAJfsQAQQAAQABAgQBACAARAAIUCFAFAIAAijQ09EyYhTTIxMTEbKc0ANqizaHqDmvi7kinChIL6rV2I")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	format, err := DetectFormat(path)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatBZIP2 {
		t.Fatalf("got format %q", format)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if text != "BZIP2 text 日本語\n" {
		t.Fatalf("unexpected BZIP2 text %q", text)
	}
}

func TestDetectExtensionlessUTF16AndEmail(t *testing.T) {
	dir := t.TempDir()
	utf16Path := filepath.Join(dir, "utf16-data")
	utf16 := []byte{0xff, 0xfe, 'H', 0, 'e', 0, 'l', 0, 'l', 0, 'o', 0}
	if err := os.WriteFile(utf16Path, utf16, 0o600); err != nil {
		t.Fatal(err)
	}
	format, err := DetectFormat(utf16Path)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatText {
		t.Fatalf("UTF-16 format = %q", format)
	}
	text, err := Read(utf16Path)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello" {
		t.Fatalf("unexpected UTF-16 text %q", text)
	}

	utf16NoBOMPath := filepath.Join(dir, "utf16-no-bom")
	utf16NoBOM := []byte{'W', 0, 'o', 0, 'r', 0, 'l', 0, 'd', 0}
	if err := os.WriteFile(utf16NoBOMPath, utf16NoBOM, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err = Read(utf16NoBOMPath)
	if err != nil {
		t.Fatal(err)
	}
	if text != "World" {
		t.Fatalf("unexpected BOM-less UTF-16 text %q", text)
	}

	binaryPath := filepath.Join(dir, "binary-data")
	if err := os.WriteFile(binaryPath, []byte{'A', 0, 1, 2, 'B', 0, 3, 4}, 0o600); err != nil {
		t.Fatal(err)
	}
	format, err = DetectFormat(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatBinary {
		t.Fatalf("binary format = %q", format)
	}

	emailPath := filepath.Join(dir, "mail-data")
	message := "From: sender@example.com\r\nSubject: extensionless\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nBody\r\n"
	if err := os.WriteFile(emailPath, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	format, err = DetectFormat(emailPath)
	if err != nil {
		t.Fatal(err)
	}
	if format != FormatEmail {
		t.Fatalf("email format = %q", format)
	}
	text, err = Read(emailPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Subject: extensionless") || !strings.Contains(text, "Body") {
		t.Fatalf("unexpected email text %q", text)
	}
}
func TestExtractTAR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := tar.NewWriter(file)
	body := []byte("tar text")
	if err := writer.WriteHeader(&tar.Header{Name: "notes.txt", Mode: 0o600, Size: int64(len(body))}); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "tar text") {
		t.Fatalf("unexpected TAR text %q", text)
	}
}

func TestExtractMIMEEmail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "message.eml")
	message := "Subject: =?UTF-8?B?44OG44K544OI?=\r\n" +
		"From: sender@example.com\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=part\r\n\r\n" +
		"--part\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nPlain body\r\n" +
		"--part\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>HTML <b>body</b></p>\r\n" +
		"--part--\r\n"
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Subject: テスト", "Plain body", "HTML body"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
}

func TestRTFUnicodeAndHiddenDestinations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.rtf")
	data := `{\rtf1\ansi{\fonttbl{\f0 Arial;}}Visible \u28450?\u35486?\par Next}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "Arial") || !strings.Contains(text, "Visible 漢語") || !strings.Contains(text, "Next") {
		t.Fatalf("unexpected RTF text %q", text)
	}
}

func TestBinaryUTF16Recovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.doc")
	data := append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1, 0, 0}, []byte{'H', 0, 'e', 0, 'l', 0, 'l', 0, 'o', 0, 0, 0}...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	text, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello") {
		t.Fatalf("unexpected binary strings %q", text)
	}
}

func encodeUTF16LE(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, 2+len(units)*2)
	data[0], data[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[2+index*2:], unit)
	}
	return data
}
func writeZIP(t *testing.T, filePath string, files map[string]string) {
	t.Helper()
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, body := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
