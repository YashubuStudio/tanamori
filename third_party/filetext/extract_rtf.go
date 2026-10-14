package filetext

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/encoding/charmap"
)

var rtfDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true,
	"pict": true, "object": true, "header": true, "footer": true,
	"headerl": true, "headerr": true, "footerl": true, "footerr": true,
	"filetbl": true, "listtable": true, "listoverridetable": true,
	"generator": true, "themedata": true, "datastore": true,
}

type rtfState struct {
	skip bool
	uc   int
}

func ExtractTextFromRTF(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if uint64(len(data)) > maxArchiveEntryBytes {
		return "", fmt.Errorf("filetext: RTF input exceeds %d bytes", maxArchiveEntryBytes)
	}
	text, err := parseRTF(data)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text), nil
}

func parseRTF(data []byte) (string, error) {
	states := []rtfState{{uc: 1}}
	var output strings.Builder
	skipFallback := 0
	for index := 0; index < len(data); {
		current := &states[len(states)-1]
		switch data[index] {
		case '{':
			states = append(states, *current)
			index++
		case '}':
			if len(states) > 1 {
				states = states[:len(states)-1]
			}
			index++
		case '\\':
			if index+1 >= len(data) {
				index++
				continue
			}
			next := data[index+1]
			if skipFallback > 0 {
				if next == '\'' && index+3 < len(data) {
					index += 4
				} else {
					index += 2
				}
				skipFallback--
				continue
			}
			switch next {
			case '\\', '{', '}':
				if !current.skip {
					output.WriteByte(next)
				}
				index += 2
				continue
			case '\'':
				if index+3 >= len(data) {
					return "", fmt.Errorf("filetext: truncated RTF hex escape")
				}
				value, err := strconv.ParseUint(string(data[index+2:index+4]), 16, 8)
				if err != nil {
					return "", fmt.Errorf("filetext: invalid RTF hex escape: %w", err)
				}
				if !current.skip {
					decoded, _ := charmap.Windows1252.NewDecoder().String(string([]byte{byte(value)}))
					output.WriteString(decoded)
				}
				index += 4
				continue
			case '*':
				current.skip = true
				index += 2
				continue
			case '~':
				if !current.skip {
					output.WriteRune('\u00a0')
				}
				index += 2
				continue
			case '_':
				if !current.skip {
					output.WriteRune('\u2011')
				}
				index += 2
				continue
			case '-':
				index += 2
				continue
			}

			wordStart := index + 1
			wordEnd := wordStart
			for wordEnd < len(data) && unicode.IsLetter(rune(data[wordEnd])) {
				wordEnd++
			}
			word := string(data[wordStart:wordEnd])
			numberStart := wordEnd
			if numberStart < len(data) && data[numberStart] == '-' {
				numberStart++
			}
			numberEnd := numberStart
			for numberEnd < len(data) && data[numberEnd] >= '0' && data[numberEnd] <= '9' {
				numberEnd++
			}
			numberText := string(data[wordEnd:numberEnd])
			index = numberEnd
			if index < len(data) && data[index] == ' ' {
				index++
			}
			if rtfDestinations[word] {
				current.skip = true
			}
			if current.skip {
				continue
			}
			switch word {
			case "par", "line":
				output.WriteByte('\n')
			case "tab":
				output.WriteByte('\t')
			case "emdash":
				output.WriteRune('\u2014')
			case "endash":
				output.WriteRune('\u2013')
			case "bullet":
				output.WriteRune('\u2022')
			case "lquote", "rquote":
				output.WriteRune('\u2019')
			case "ldblquote", "rdblquote":
				output.WriteRune('\u201d')
			case "uc":
				if value, err := strconv.Atoi(numberText); err == nil && value >= 0 {
					current.uc = value
				}
			case "u":
				if value, err := strconv.Atoi(numberText); err == nil {
					if value < 0 {
						value += 65536
					}
					output.WriteRune(rune(value))
					skipFallback = current.uc
				}
			case "bin":
				if value, err := strconv.Atoi(numberText); err == nil && value > 0 {
					index += value
					if index > len(data) {
						index = len(data)
					}
				}
			}
		default:
			if skipFallback > 0 {
				skipFallback--
				index++
				continue
			}
			if !current.skip && data[index] >= 0x20 {
				if data[index] < 0x80 {
					output.WriteByte(data[index])
				} else {
					decoded, _ := charmap.Windows1252.NewDecoder().String(string([]byte{data[index]}))
					output.WriteString(decoded)
				}
			}
			index++
		}
	}
	return output.String(), nil
}
