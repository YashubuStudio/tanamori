package filetext

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ExtractTextFromODF extracts text from OpenDocument text, spreadsheet, and
// presentation containers (ODT/ODS/ODP and their template variants).
func ExtractTextFromODF(path string) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) > maxArchiveEntries {
		return "", fmt.Errorf("filetext: ODF has too many entries: %d", len(archive.File))
	}
	for _, file := range archive.File {
		if strings.EqualFold(file.Name, "content.xml") {
			if file.UncompressedSize64 > maxArchiveEntryBytes {
				return "", fmt.Errorf("filetext: ODF content exceeds %d bytes", maxArchiveEntryBytes)
			}
			reader, err := file.Open()
			if err != nil {
				return "", err
			}
			text, extractErr := extractXMLText(reader)
			closeErr := reader.Close()
			if extractErr != nil {
				return "", extractErr
			}
			if closeErr != nil {
				return "", closeErr
			}
			return text, nil
		}
	}
	return "", fmt.Errorf("filetext: ODF content.xml was not found")
}

// ExtractTextFromODT is retained for source compatibility.
func ExtractTextFromODT(path string) (string, error) {
	return ExtractTextFromODF(path)
}

func extractXMLText(reader io.Reader) (string, error) {
	decoder := xml.NewDecoder(reader)
	var output strings.Builder
	pendingSpace := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.CharData:
			text := strings.TrimSpace(string(value))
			if text == "" {
				continue
			}
			if pendingSpace && output.Len() > 0 {
				output.WriteByte(' ')
			}
			output.WriteString(text)
			pendingSpace = true
		case xml.StartElement:
			switch value.Name.Local {
			case "tab":
				output.WriteByte('\t')
				pendingSpace = false
			case "line-break":
				output.WriteByte('\n')
				pendingSpace = false
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "p", "h", "table-row", "list-item":
				if output.Len() > 0 {
					output.WriteByte('\n')
				}
				pendingSpace = false
			case "table-cell":
				output.WriteByte('\t')
				pendingSpace = false
			}
		}
	}
	return strings.TrimSpace(output.String()), nil
}
