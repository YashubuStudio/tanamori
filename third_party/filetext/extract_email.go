package filetext

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/net/html/charset"
)

// ExtractTextFromEmail extracts common headers and readable MIME body parts
// from EML, MHT, and MHTML files.
func ExtractTextFromEmail(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	message, err := mail.ReadMessage(file)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, name := range []string{"Subject", "From", "To", "Cc", "Date"} {
		if value := decodeMIMEHeader(message.Header.Get(name)); value != "" {
			output.WriteString(name)
			output.WriteString(": ")
			output.WriteString(value)
			output.WriteByte('\n')
		}
	}
	if output.Len() > 0 {
		output.WriteByte('\n')
	}
	total := int64(0)
	body, err := extractMIMEPart(textproto.MIMEHeader(message.Header), message.Body, 0, &total)
	if err != nil {
		return "", err
	}
	output.WriteString(strings.TrimSpace(body))
	return output.String(), nil
}

func extractMIMEPart(header textproto.MIMEHeader, body io.Reader, depth int, total *int64) (string, error) {
	if depth > 16 {
		return "", fmt.Errorf("filetext: MIME nesting exceeds 16 levels")
	}
	body = decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding"))
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", fmt.Errorf("filetext: multipart MIME body has no boundary")
		}
		reader := multipart.NewReader(body, boundary)
		var output strings.Builder
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			text, partErr := extractMIMEPart(part.Header, part, depth+1, total)
			part.Close()
			if partErr != nil {
				return "", partErr
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
	if mediaType == "message/rfc822" {
		message, err := mail.ReadMessage(body)
		if err != nil {
			return "", err
		}
		return extractMIMEPart(textproto.MIMEHeader(message.Header), message.Body, depth+1, total)
	}

	fileName := decodeMIMEHeader(params["name"])
	if disposition := header.Get("Content-Disposition"); disposition != "" {
		if _, dispositionParams, err := mime.ParseMediaType(disposition); err == nil && dispositionParams["filename"] != "" {
			fileName = decodeMIMEHeader(dispositionParams["filename"])
		}
	}
	readable := strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" ||
		mediaType == "application/xml" || mediaType == "application/yaml"
	if !readable && fileName != "" {
		format, ok := formatFromPath(fileName)
		readable = ok && isDirectTextFormat(format)
	}
	if !readable {
		return "", nil
	}

	data, err := readMIMEData(body, total)
	if err != nil {
		return "", err
	}
	decoded := string(data)
	if label := params["charset"]; label != "" {
		reader, err := charset.NewReaderLabel(label, bytes.NewReader(data))
		if err == nil {
			converted, readErr := io.ReadAll(reader)
			if readErr != nil {
				return "", readErr
			}
			decoded = string(converted)
		}
	} else if converted, err := decodeTextBytes(data); err == nil {
		decoded = converted
	}
	if mediaType == "text/html" {
		decoded = stripHTMLTags(decoded)
	}
	if fileName != "" {
		return "===== " + filepath.ToSlash(fileName) + " =====\n" + decoded, nil
	}
	return decoded, nil
}

func isDirectTextFormat(format Format) bool {
	switch format {
	case FormatText, FormatHTML, FormatXML, FormatJSON, FormatJSONL, FormatYAML,
		FormatTOML, FormatINI, FormatCSV, FormatTSV, FormatSRT, FormatVTT,
		FormatTeX, FormatBibTeX:
		return true
	default:
		return false
	}
}

func decodeTransferEncoding(reader io.Reader, value string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, reader)
	case "quoted-printable":
		return quotedprintable.NewReader(reader)
	default:
		return reader
	}
}

func readMIMEData(reader io.Reader, total *int64) ([]byte, error) {
	remaining := int64(maxArchiveTotalBytes) - *total
	if remaining <= 0 {
		return nil, fmt.Errorf("filetext: MIME decoded data exceeds %d bytes", maxArchiveTotalBytes)
	}
	data, err := io.ReadAll(io.LimitReader(reader, remaining+1))
	if err != nil {
		return nil, err
	}
	*total += int64(len(data))
	if *total > int64(maxArchiveTotalBytes) {
		return nil, fmt.Errorf("filetext: MIME decoded data exceeds %d bytes", maxArchiveTotalBytes)
	}
	return data, nil
}

func decodeMIMEHeader(value string) string {
	decoded, err := new(mime.WordDecoder).DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}
