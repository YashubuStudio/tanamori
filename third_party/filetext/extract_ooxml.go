package filetext

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var numericPart = regexp.MustCompile(`(\d+)`)

func ExtractTextFromDocx(path string) (string, error) {
	return extractOOXML(path, func(name string) (int, bool) {
		name = strings.ToLower(name)
		switch {
		case name == "word/document.xml":
			return 0, true
		case name == "word/footnotes.xml":
			return 1, true
		case name == "word/endnotes.xml":
			return 2, true
		case name == "word/comments.xml":
			return 3, true
		case strings.HasPrefix(name, "word/header") && strings.HasSuffix(name, ".xml"):
			return 4, true
		case strings.HasPrefix(name, "word/footer") && strings.HasSuffix(name, ".xml"):
			return 5, true
		default:
			return 0, false
		}
	})
}

func ExtractTextFromPPTX(path string) (string, error) {
	return extractOOXML(path, func(name string) (int, bool) {
		name = strings.ToLower(name)
		switch {
		case strings.HasPrefix(name, "ppt/slides/slide") && strings.HasSuffix(name, ".xml"):
			return 0, true
		case strings.HasPrefix(name, "ppt/notesslides/notesslide") && strings.HasSuffix(name, ".xml"):
			return 1, true
		default:
			return 0, false
		}
	})
}

type ooxmlPart struct {
	file *zip.File
	rank int
}

func extractOOXML(path string, include func(string) (int, bool)) (string, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) > maxArchiveEntries {
		return "", fmt.Errorf("filetext: archive has too many entries: %d", len(archive.File))
	}

	parts := make([]ooxmlPart, 0)
	var total uint64
	for _, file := range archive.File {
		if rank, ok := include(file.Name); ok {
			if file.UncompressedSize64 > maxArchiveEntryBytes {
				return "", fmt.Errorf("filetext: archive entry %q is too large", file.Name)
			}
			total += file.UncompressedSize64
			if total > maxArchiveTotalBytes {
				return "", fmt.Errorf("filetext: Office XML data exceeds %d bytes", maxArchiveTotalBytes)
			}
			parts = append(parts, ooxmlPart{file: file, rank: rank})
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("filetext: no readable Office XML parts found")
	}
	sort.SliceStable(parts, func(i, j int) bool {
		if parts[i].rank != parts[j].rank {
			return parts[i].rank < parts[j].rank
		}
		left, right := partNumber(parts[i].file.Name), partNumber(parts[j].file.Name)
		if left != right {
			return left < right
		}
		return parts[i].file.Name < parts[j].file.Name
	})

	var output strings.Builder
	for _, part := range parts {
		reader, err := part.file.Open()
		if err != nil {
			return "", err
		}
		text, extractErr := extractOfficeXML(reader)
		closeErr := reader.Close()
		if extractErr != nil {
			return "", extractErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		text = strings.TrimSpace(text)
		if text != "" {
			if output.Len() > 0 {
				output.WriteString("\n\n")
			}
			output.WriteString(text)
		}
	}
	return output.String(), nil
}

func extractOfficeXML(reader io.Reader) (string, error) {
	decoder := xml.NewDecoder(reader)
	var output strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch value := token.(type) {
		case xml.StartElement:
			switch value.Name.Local {
			case "t", "instrText", "delText":
				var text string
				if err := decoder.DecodeElement(&text, &value); err != nil {
					return "", err
				}
				output.WriteString(text)
			case "tab":
				output.WriteByte('\t')
			case "br":
				output.WriteByte('\n')
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "p", "tr":
				output.WriteByte('\n')
			case "tc":
				output.WriteByte('\t')
			}
		}
	}
	return output.String(), nil
}

func partNumber(name string) int {
	matches := numericPart.FindAllString(name, -1)
	if len(matches) == 0 {
		return 0
	}
	number, _ := strconv.Atoi(matches[len(matches)-1])
	return number
}
