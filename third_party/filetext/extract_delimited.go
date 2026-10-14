package filetext

import (
	"encoding/csv"
	"io"
	"strings"
)

func ExtractTextFromCSV(path string) (string, error) {
	return extractDelimited(path, ',')
}

// ExtractTextFromTSV extracts tab-separated rows while preserving columns.
func ExtractTextFromTSV(path string) (string, error) {
	return extractDelimited(path, '\t')
}

func extractDelimited(path string, separator rune) (string, error) {
	decoded, closeFn, err := openDecodedText(path)
	if err != nil {
		return "", err
	}
	defer closeFn()

	reader := csv.NewReader(decoded)
	reader.Comma = separator
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	var output strings.Builder
	first := true
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if !first {
			output.WriteByte('\n')
		}
		first = false
		output.WriteString(strings.Join(record, "\t"))
	}
	return output.String(), nil
}
