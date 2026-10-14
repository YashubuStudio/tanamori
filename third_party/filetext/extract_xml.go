package filetext

import (
	"encoding/xml"
	"io"
	"strings"
)

func ExtractTextFromXML(path string) (string, error) {
	reader, closeFn, err := openDecodedText(path)
	if err != nil {
		return "", err
	}
	defer closeFn()

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
		if data, ok := token.(xml.CharData); ok {
			text := strings.TrimSpace(string(data))
			if text != "" {
				if output.Len() > 0 {
					output.WriteByte(' ')
				}
				output.WriteString(text)
			}
		}
	}
	return output.String(), nil
}
