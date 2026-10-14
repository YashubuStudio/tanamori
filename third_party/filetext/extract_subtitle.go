package filetext

import (
	"strconv"
	"strings"
)

func ExtractTextFromSRT(path string) (string, error) {
	content, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isSRTMetaLine(line) {
			continue
		}
		sb.WriteString(line + " ")
	}
	return sb.String(), nil
}

func isSRTMetaLine(line string) bool {
	return strings.Contains(line, "-->") || isNumeric(line)
}

func isNumeric(s string) bool {
	_, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil
}

func ExtractTextFromTextLog(path string) (string, error) {
	txt, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(txt, "\n")
	var sb strings.Builder
	for _, line := range lines {
		// 時刻などを除く：任意実装。ここでは全行対象。
		line = strings.TrimSpace(line)
		if line != "" {
			sb.WriteString(line + " ")
		}
	}
	return sb.String(), nil
}

func ExtractTextFromVTT(path string) (string, error) {
	content, err := ReadTextFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(content, "\n")
	var sb strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isVTTMetaLine(line) {
			continue
		}
		sb.WriteString(line + " ")
	}
	return sb.String(), nil
}

func isVTTMetaLine(line string) bool {
	// "WEBVTT" ヘッダー、時刻行、NOTE行をスキップ
	return strings.HasPrefix(line, "WEBVTT") ||
		strings.HasPrefix(line, "NOTE") ||
		strings.Contains(line, "-->") ||
		isNumeric(line)
}
