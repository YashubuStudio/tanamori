package filetext

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Selection describes a format-independent selection pipeline. Line numbers
// are one-based. Filters are applied in this order: delimiters, line ranges,
// exclude regexp, match regexp with context, head/tail, and character limit.
type Selection struct {
	Lines      string
	Match      string
	Exclude    string
	Before     int
	After      int
	Head       int
	Tail       int
	From       string
	To         string
	MaxChars   int
	IgnoreCase bool
}

type lineRange struct {
	start int
	end   int
}

type selectedLine struct {
	number int
	text   string
}

func writeSelected(ctx context.Context, src io.Reader, dst io.Writer, selection Selection) error {
	if selection.Before < 0 || selection.After < 0 || selection.Head < 0 || selection.Tail < 0 || selection.MaxChars < 0 {
		return fmt.Errorf("filetext: selection counts must not be negative")
	}
	if selection.Head > 0 && selection.Tail > 0 {
		return fmt.Errorf("filetext: head and tail cannot be used together")
	}

	ranges, err := parseLineRanges(selection.Lines)
	if err != nil {
		return err
	}
	match, err := compilePattern(selection.Match, selection.IgnoreCase)
	if err != nil {
		return fmt.Errorf("filetext: invalid match regexp: %w", err)
	}
	exclude, err := compilePattern(selection.Exclude, selection.IgnoreCase)
	if err != nil {
		return fmt.Errorf("filetext: invalid exclude regexp: %w", err)
	}

	from := selection.From
	to := selection.To
	if selection.IgnoreCase {
		from = strings.ToLower(from)
		to = strings.ToLower(to)
	}

	reader := bufio.NewReader(src)
	started := from == ""
	lineNumber := 0
	afterRemaining := 0
	lastEmitted := 0
	emittedCount := 0
	remainingChars := selection.MaxChars
	before := make([]selectedLine, 0, selection.Before)
	tail := make([]selectedLine, 0, selection.Tail)

	writeLine := func(line selectedLine) (bool, error) {
		if line.number <= lastEmitted {
			return false, nil
		}
		lastEmitted = line.number
		emittedCount++
		if selection.Tail > 0 {
			if len(tail) == selection.Tail {
				copy(tail, tail[1:])
				tail[len(tail)-1] = line
			} else {
				tail = append(tail, line)
			}
			return false, nil
		}
		stop, err := writeWithRuneLimit(dst, line.text, &remainingChars)
		if err != nil || stop {
			return true, err
		}
		return selection.Head > 0 && emittedCount >= selection.Head, nil
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadString('\n')
		if len(line) == 0 && readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.EOF {
			return readErr
		}
		lineNumber++
		probe := strings.TrimRight(line, "\r\n")
		compare := probe
		if selection.IgnoreCase {
			compare = strings.ToLower(compare)
		}

		if !started {
			if !strings.Contains(compare, from) {
				if readErr == io.EOF {
					break
				}
				continue
			}
			started = true
		}
		stopAtThisLine := to != "" && strings.Contains(compare, to)
		eligible := inLineRanges(lineNumber, ranges) && (exclude == nil || !exclude.MatchString(probe))

		if eligible {
			current := selectedLine{number: lineNumber, text: line}
			if match == nil {
				stop, err := writeLine(current)
				if err != nil {
					return err
				}
				if stop {
					break
				}
			} else if match.MatchString(probe) {
				for _, previous := range before {
					stop, err := writeLine(previous)
					if err != nil {
						return err
					}
					if stop {
						return flushTail(dst, tail, &remainingChars)
					}
				}
				before = before[:0]
				stop, err := writeLine(current)
				if err != nil {
					return err
				}
				if stop {
					break
				}
				afterRemaining = selection.After
			} else if afterRemaining > 0 {
				stop, err := writeLine(current)
				if err != nil {
					return err
				}
				if stop {
					break
				}
				afterRemaining--
			} else if selection.Before > 0 {
				if len(before) == selection.Before {
					copy(before, before[1:])
					before[len(before)-1] = current
				} else {
					before = append(before, current)
				}
			}
		}

		if stopAtThisLine || readErr == io.EOF {
			break
		}
	}
	return flushTail(dst, tail, &remainingChars)
}

func compilePattern(pattern string, ignoreCase bool) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	if ignoreCase {
		pattern = "(?i:" + pattern + ")"
	}
	return regexp.Compile(pattern)
}

func parseLineRanges(spec string) ([]lineRange, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	var ranges []lineRange
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("filetext: empty line range")
		}
		if !strings.Contains(part, "-") {
			n, err := positiveInt(part)
			if err != nil {
				return nil, fmt.Errorf("filetext: invalid line %q", part)
			}
			ranges = append(ranges, lineRange{start: n, end: n})
			continue
		}
		pieces := strings.SplitN(part, "-", 2)
		start, end := 1, int(^uint(0)>>1)
		var err error
		if pieces[0] != "" {
			start, err = positiveInt(pieces[0])
			if err != nil {
				return nil, fmt.Errorf("filetext: invalid line range %q", part)
			}
		}
		if pieces[1] != "" {
			end, err = positiveInt(pieces[1])
			if err != nil || end < start {
				return nil, fmt.Errorf("filetext: invalid line range %q", part)
			}
		}
		ranges = append(ranges, lineRange{start: start, end: end})
	}
	return ranges, nil
}

func positiveInt(value string) (int, error) {
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("must be a positive integer")
	}
	return n, nil
}

func inLineRanges(line int, ranges []lineRange) bool {
	if len(ranges) == 0 {
		return true
	}
	for _, candidate := range ranges {
		if line >= candidate.start && line <= candidate.end {
			return true
		}
	}
	return false
}

func writeWithRuneLimit(dst io.Writer, text string, remaining *int) (bool, error) {
	if *remaining == 0 {
		_, err := io.WriteString(dst, text)
		return false, err
	}
	if utf8.RuneCountInString(text) <= *remaining {
		_, err := io.WriteString(dst, text)
		*remaining -= utf8.RuneCountInString(text)
		return *remaining == 0, err
	}
	end := 0
	for index := range text {
		if *remaining == 0 {
			break
		}
		end = index
		*remaining--
	}
	if *remaining == 0 {
		_, size := utf8.DecodeRuneInString(text[end:])
		end += size
	}
	_, err := io.WriteString(dst, text[:end])
	return true, err
}

func flushTail(dst io.Writer, tail []selectedLine, remaining *int) error {
	for _, line := range tail {
		stop, err := writeWithRuneLimit(dst, line.text, remaining)
		if err != nil {
			return err
		}
		if stop {
			break
		}
	}
	return nil
}
