package filetext

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf16"
)

const minimumBinaryStringRunes = 4

// ExtractTextFromBinary performs a best-effort recovery of printable ASCII and
// UTF-16 strings. It is intended for legacy OLE Office and MSG files, not as a
// replacement for a full fidelity legacy document parser.
func ExtractTextFromBinary(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxArchiveEntryBytes)+1))
	if err != nil {
		return "", err
	}
	if uint64(len(data)) > maxArchiveEntryBytes {
		return "", fmt.Errorf("filetext: binary input exceeds %d bytes", maxArchiveEntryBytes)
	}

	seen := make(map[string]struct{})
	var values []string
	add := func(value string) {
		value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
		if len([]rune(value)) < minimumBinaryStringRunes {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	extractASCIIStrings(data, add)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for offset := 0; offset < 2; offset++ {
			extractUTF16Strings(data[offset:], order, add)
		}
	}
	return strings.Join(values, "\n"), nil
}

func extractASCIIStrings(data []byte, add func(string)) {
	start := -1
	for index, value := range data {
		printable := value == '\t' || value == '\n' || value == '\r' || (value >= 0x20 && value <= 0x7e)
		if printable {
			if start < 0 {
				start = index
			}
			continue
		}
		if start >= 0 {
			add(string(data[start:index]))
			start = -1
		}
	}
	if start >= 0 {
		add(string(data[start:]))
	}
}

func extractUTF16Strings(data []byte, order binary.ByteOrder, add func(string)) {
	units := make([]uint16, 0, 64)
	flush := func() {
		if len(units) >= minimumBinaryStringRunes {
			add(string(utf16.Decode(units)))
		}
		units = units[:0]
	}
	for index := 0; index+1 < len(data); index += 2 {
		unit := order.Uint16(data[index : index+2])
		r := rune(unit)
		if unit >= 0xd800 && unit <= 0xdfff || unicode.IsPrint(r) || unicode.IsSpace(r) {
			units = append(units, unit)
		} else {
			flush()
		}
	}
	flush()
}
