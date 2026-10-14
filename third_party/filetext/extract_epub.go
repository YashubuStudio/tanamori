package filetext

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

type epubContainer struct {
	RootFiles []struct {
		FullPath string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type epubPackage struct {
	Manifest []struct {
		ID        string `xml:"id,attr"`
		Href      string `xml:"href,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

func ExtractTextFromEPUB(filePath string) (string, error) {
	archive, err := zip.OpenReader(filePath)
	if err != nil {
		return "", err
	}
	defer archive.Close()
	if len(archive.File) > maxArchiveEntries {
		return "", fmt.Errorf("filetext: EPUB has too many entries: %d", len(archive.File))
	}
	files := make(map[string]*zip.File, len(archive.File))
	for _, file := range archive.File {
		files[path.Clean(file.Name)] = file
	}
	ordered := epubReadingOrder(files)
	if len(ordered) == 0 {
		for name := range files {
			lower := strings.ToLower(name)
			if strings.HasSuffix(lower, ".xhtml") || strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm") {
				ordered = append(ordered, name)
			}
		}
		sort.Strings(ordered)
	}

	var output strings.Builder
	var total uint64
	for _, name := range ordered {
		file := files[path.Clean(name)]
		if file == nil {
			continue
		}
		if file.UncompressedSize64 > maxArchiveEntryBytes {
			return "", fmt.Errorf("filetext: EPUB entry %q is too large", name)
		}
		total += file.UncompressedSize64
		if total > maxArchiveTotalBytes {
			return "", fmt.Errorf("filetext: EPUB text data exceeds %d bytes", maxArchiveTotalBytes)
		}
		data, err := readZIPEntry(file)
		if err != nil {
			return "", err
		}
		decoded, err := decodeTextBytes(data)
		if err != nil {
			return "", err
		}
		text := strings.TrimSpace(stripHTMLTags(decoded))
		if text != "" {
			if output.Len() > 0 {
				output.WriteString("\n\n")
			}
			output.WriteString(text)
		}
	}
	return output.String(), nil
}

func epubReadingOrder(files map[string]*zip.File) []string {
	containerFile := files["META-INF/container.xml"]
	if containerFile == nil {
		containerFile = files["meta-inf/container.xml"]
	}
	if containerFile == nil {
		return nil
	}
	containerData, err := readZIPEntry(containerFile)
	if err != nil {
		return nil
	}
	var container epubContainer
	if xml.Unmarshal(containerData, &container) != nil || len(container.RootFiles) == 0 {
		return nil
	}
	packagePath := path.Clean(container.RootFiles[0].FullPath)
	packageFile := files[packagePath]
	if packageFile == nil {
		return nil
	}
	packageData, err := readZIPEntry(packageFile)
	if err != nil {
		return nil
	}
	var book epubPackage
	if xml.Unmarshal(packageData, &book) != nil {
		return nil
	}
	manifest := make(map[string]string, len(book.Manifest))
	for _, item := range book.Manifest {
		manifest[item.ID] = path.Clean(path.Join(path.Dir(packagePath), item.Href))
	}
	ordered := make([]string, 0, len(book.Spine))
	for _, item := range book.Spine {
		if name := manifest[item.IDRef]; name != "" {
			ordered = append(ordered, name)
		}
	}
	return ordered
}

func readZIPEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, int64(maxArchiveEntryBytes)+1))
	if err != nil {
		return nil, err
	}
	if uint64(len(data)) > maxArchiveEntryBytes {
		return nil, fmt.Errorf("filetext: ZIP entry %q exceeds %d bytes", file.Name, maxArchiveEntryBytes)
	}
	return data, nil
}
