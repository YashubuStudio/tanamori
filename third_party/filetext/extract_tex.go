package filetext

import (
	"regexp"
	"strings"
)

var texCommand = regexp.MustCompile(`\\[A-Za-z@]+\*?`)

func ExtractTextFromTEX(filePath string) (string, error) {
	content, err := ReadTextFile(filePath)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, line := range strings.Split(content, "\n") {
		line = stripTeXComment(line)
		line = texCommand.ReplaceAllString(line, " ")
		line = strings.NewReplacer("{", " ", "}", " ", "~", " ").Replace(line)
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			if output.Len() > 0 {
				output.WriteByte('\n')
			}
			output.WriteString(line)
		}
	}
	return output.String(), nil
}

func stripTeXComment(line string) string {
	for index := 0; index < len(line); index++ {
		if line[index] != '%' {
			continue
		}
		backslashes := 0
		for previous := index - 1; previous >= 0 && line[previous] == '\\'; previous-- {
			backslashes++
		}
		if backslashes%2 == 0 {
			return line[:index]
		}
	}
	return line
}

func ExtractTextFromBIB(filePath string) (string, error) {
	content, err := ReadTextFile(filePath)
	if err != nil {
		return "", err
	}
	content = strings.NewReplacer("{", " ", "}", " ", "\n", " ").Replace(content)
	return strings.Join(strings.Fields(content), " "), nil
}
