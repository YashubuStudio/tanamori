package scoring

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/YashubuStudio/tanamori/internal/app"
)

// KeywordScorer is a deterministic offline baseline. It is intentionally not
// presented as an AI model; callers can replace it with ONNX or llama.cpp
// without changing the application service.
type KeywordScorer struct{}

func (KeywordScorer) Score(ctx context.Context, request app.ScoreRequest) (app.ScoreResult, error) {
	started := time.Now()
	if err := ctx.Err(); err != nil {
		return app.ScoreResult{}, err
	}
	leftText := request.LeftText
	if fileName := strings.TrimSpace(request.Metadata["file_name"]); fileName != "" {
		leftText = fileName + "\n" + leftText
	}
	left := terms(leftText)
	right := terms(request.RightText)
	if len(left) == 0 || len(right) == 0 {
		return app.ScoreResult{ModelVersion: "keyword-baseline-v1", Duration: time.Since(started)}, nil
	}
	matches := 0
	for term := range right {
		if _, ok := left[term]; ok {
			matches++
		}
	}
	coverage := float64(matches) / float64(len(right))
	if coverage > 1 {
		coverage = 1
	}
	reason := "キーワード一致なし"
	if matches > 0 {
		reason = fmt.Sprintf("フォルダ説明の語句が%d件一致", matches)
	}
	return app.ScoreResult{
		Score: coverage, Reason: reason, ModelVersion: "keyword-baseline-v1", Duration: time.Since(started),
	}, nil
}

func terms(value string) map[string]struct{} {
	value = strings.ToLower(value)
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return ' '
	}, value)
	result := make(map[string]struct{})
	for _, field := range strings.Fields(cleaned) {
		runes := []rune(field)
		result[field] = struct{}{}
		if len(runes) > 2 {
			for index := 0; index+1 < len(runes); index++ {
				result[string(runes[index:index+2])] = struct{}{}
			}
		}
	}
	return result
}
