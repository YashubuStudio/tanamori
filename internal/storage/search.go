package storage

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

func (db *DB) Search(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	ftsQuery := buildFTSQuery(query)
	if ftsQuery == "" {
		return nil, fmt.Errorf("search query must not be empty")
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	rows, err := db.sql.QueryContext(ctx, `
SELECT file_id, raw_name, raw_path, raw_body,
       bm25(file_search, 0.0, 4.0, 1.5, 1.0, 0.5, 1.0, 0.0, 0.0, 0.0)
FROM file_search
WHERE file_search MATCH ?
ORDER BY bm25(file_search, 0.0, 4.0, 1.5, 1.0, 0.5, 1.0, 0.0, 0.0, 0.0)
LIMIT ?`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("search files: %w", err)
	}
	defer rows.Close()
	var hits []domain.SearchHit
	for rows.Next() {
		var hit domain.SearchHit
		var body string
		if err := rows.Scan(&hit.FileID, &hit.Name, &hit.Path, &body, &hit.Rank); err != nil {
			return nil, fmt.Errorf("scan search hit: %w", err)
		}
		hit.Snippet = makeSnippet(body, query, 240)
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search hits: %w", err)
	}
	return hits, nil
}

func buildFTSQuery(query string) string {
	fields := strings.Fields(strings.TrimSpace(query))
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		tokens := cjkSearchTokens(field)
		if len(tokens) == 0 {
			tokens = []string{field}
		}
		for _, token := range tokens {
			token = strings.ReplaceAll(token, `"`, `""`)
			if token != "" {
				quoted = append(quoted, `"`+token+`"`)
			}
		}
	}
	return strings.Join(quoted, " AND ")
}

func augmentForFTS(value string) string {
	tokens := cjkIndexTokens(value)
	if len(tokens) == 0 {
		return value
	}
	return value + " " + strings.Join(tokens, " ")
}

func cjkSearchTokens(value string) []string {
	var result []string
	for _, run := range cjkRuns(value) {
		if len(run) == 1 {
			result = append(result, string(run))
			continue
		}
		for index := 0; index+1 < len(run); index++ {
			result = append(result, string(run[index:index+2]))
		}
	}
	return uniqueStrings(result)
}

func cjkIndexTokens(value string) []string {
	var result []string
	for _, run := range cjkRuns(value) {
		for _, character := range run {
			result = append(result, string(character))
		}
		for index := 0; index+1 < len(run); index++ {
			result = append(result, string(run[index:index+2]))
		}
	}
	return uniqueStrings(result)
}

func cjkRuns(value string) [][]rune {
	var runs [][]rune
	var current []rune
	flush := func() {
		if len(current) > 0 {
			runs = append(runs, current)
			current = nil
		}
	}
	for _, character := range value {
		if unicode.In(character, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			current = append(current, character)
		} else {
			flush()
		}
	}
	flush()
	return runs
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func makeSnippet(body, query string, maximum int) string {
	body = strings.TrimSpace(body)
	if body == "" || maximum <= 0 {
		return ""
	}
	lowerBody := strings.ToLower(body)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	start := strings.Index(lowerBody, lowerQuery)
	if start < 0 {
		start = 0
	}
	runes := []rune(body)
	runeStart := len([]rune(body[:start]))
	if runeStart > maximum/3 {
		runeStart -= maximum / 3
	} else {
		runeStart = 0
	}
	end := runeStart + maximum
	if end > len(runes) {
		end = len(runes)
	}
	snippet := string(runes[runeStart:end])
	if runeStart > 0 {
		snippet = "…" + snippet
	}
	if end < len(runes) {
		snippet += "…"
	}
	return snippet
}
