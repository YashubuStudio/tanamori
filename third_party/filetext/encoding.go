package filetext

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"github.com/saintfish/chardet"
	"golang.org/x/net/html/charset"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

const encodingSampleSize = 64 * 1024

// DetectEncoding detects the character encoding of a byte sample.
func DetectEncoding(sample []byte) encoding.Encoding {
	if bytes.HasPrefix(sample, []byte{0xff, 0xfe}) {
		return unicode.UTF16(unicode.LittleEndian, unicode.UseBOM)
	}
	if bytes.HasPrefix(sample, []byte{0xfe, 0xff}) {
		return unicode.UTF16(unicode.BigEndian, unicode.UseBOM)
	}
	if bytes.HasPrefix(sample, []byte{0xEF, 0xBB, 0xBF}) {
		return unicode.UTF8BOM
	}
	if looksLikeUTF16(sample) {
		var evenNUL, oddNUL int
		for index, value := range sample {
			if value == 0 && index%2 == 0 {
				evenNUL++
			} else if value == 0 {
				oddNUL++
			}
		}
		if oddNUL > evenNUL {
			return unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
		}
		return unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)
	}
	if validUTF8Sample(sample) {
		return unicode.UTF8
	}
	if result, err := chardet.NewTextDetector().DetectBest(sample); err == nil {
		if detected, err := ianaindex.MIME.Encoding(result.Charset); err == nil && detected != nil {
			return detected
		}
	}
	detected, _, _ := charset.DetermineEncoding(sample, "")
	return detected
}

func validUTF8Sample(sample []byte) bool {
	if utf8.Valid(sample) {
		return true
	}
	for cut := 1; cut < utf8.UTFMax && cut < len(sample); cut++ {
		prefix := sample[:len(sample)-cut]
		suffix := sample[len(sample)-cut:]
		if utf8.Valid(prefix) && !utf8.FullRune(suffix) {
			return true
		}
	}
	return false
}

func openDecodedText(path string) (io.Reader, func() error, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	sample := make([]byte, encodingSampleSize)
	n, readErr := file.Read(sample)
	if readErr != nil && readErr != io.EOF {
		file.Close()
		return nil, nil, readErr
	}
	sample = sample[:n]
	detected := DetectEncoding(sample)
	if detected == nil {
		file.Close()
		return nil, nil, fmt.Errorf("filetext: could not detect the encoding of %s", path)
	}
	decoded := transform.NewReader(io.MultiReader(bytes.NewReader(sample), file), detected.NewDecoder())
	return decoded, file.Close, nil
}

// ReadTextFile decodes a text file to UTF-8. Use Write for bounded-memory
// extraction when the complete string is not needed in memory.
func ReadTextFile(path string) (string, error) {
	reader, closeFn, err := openDecodedText(path)
	if err != nil {
		return "", err
	}
	defer closeFn()
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}
