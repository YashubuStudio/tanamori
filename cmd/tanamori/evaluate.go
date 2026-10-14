package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YashubuStudio/tanamori/internal/app"
	"github.com/YashubuStudio/tanamori/internal/domain"
	"github.com/YashubuStudio/tanamori/internal/extract"
	"github.com/YashubuStudio/tanamori/internal/filesystem"
	"github.com/YashubuStudio/tanamori/internal/modelsetup"
	"github.com/YashubuStudio/tanamori/internal/scoring"
	"github.com/YashubuStudio/tanamori/internal/storage"
)

type evaluationCase struct {
	Path      string
	Label     string
	Tags      []string
	TagsKnown bool
}

type evaluationRow struct {
	Path          string   `json:"path"`
	Expected      string   `json:"expected"`
	TopCandidate  string   `json:"top_candidate,omitempty"`
	Predicted     string   `json:"predicted,omitempty"`
	Action        string   `json:"action"`
	Score         float64  `json:"score"`
	ExpectedTags  []string `json:"expected_tags,omitempty"`
	PredictedTags []string `json:"predicted_tags,omitempty"`
	TagsKnown     bool     `json:"-"`
}

type evaluationLabel struct {
	Label     string  `json:"label"`
	TP        int     `json:"tp"`
	FP        int     `json:"fp"`
	FN        int     `json:"fn"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

type evaluationReport struct {
	Model        string            `json:"model"`
	Total        int               `json:"total"`
	Correct      int               `json:"correct"`
	Abstained    int               `json:"abstained"`
	Accuracy     float64           `json:"accuracy"`
	Coverage     float64           `json:"coverage"`
	TagSamples   int               `json:"tag_samples"`
	TagPrecision float64           `json:"tag_precision"`
	TagRecall    float64           `json:"tag_recall"`
	TagF1        float64           `json:"tag_f1"`
	TagExact     float64           `json:"tag_exact"`
	Labels       []evaluationLabel `json:"labels"`
	Cases        []evaluationRow   `json:"cases"`
}

func readEvaluationCases(path string) ([]evaluationCase, string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, "", fmt.Errorf("read evaluation CSV: %w", err)
	}
	if len(rows) < 2 {
		return nil, "", fmt.Errorf("evaluation CSV must contain a header and cases")
	}
	columns := make(map[string]int)
	for index, name := range rows[0] {
		columns[strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))] = index
	}
	pathColumn, hasPath := columns["relative_path"]
	labelColumn, hasLabel := columns["expected_label"]
	if !hasPath || !hasLabel {
		return nil, "", fmt.Errorf("evaluation CSV requires relative_path and expected_label columns")
	}
	tagColumn, hasTags := columns["expected_tags"]
	cases := make([]evaluationCase, 0, len(rows)-1)
	seen := make(map[string]bool)
	for number, row := range rows[1:] {
		if pathColumn >= len(row) || labelColumn >= len(row) {
			return nil, "", fmt.Errorf("CSV row %d is missing required columns", number+2)
		}
		relative := filepath.Clean(strings.TrimSpace(row[pathColumn]))
		label := strings.TrimSpace(row[labelColumn])
		if !filepath.IsLocal(relative) || filepath.Dir(relative) == "." || label == "" || seen[relative] {
			return nil, "", fmt.Errorf("CSV row %d has invalid path or label, or duplicate path", number+2)
		}
		seen[relative] = true
		item := evaluationCase{Path: relative, Label: label, TagsKnown: hasTags}
		if hasTags && tagColumn < len(row) {
			for _, tag := range strings.Split(row[tagColumn], "|") {
				if tag = strings.TrimSpace(tag); tag != "" {
					item.Tags = append(item.Tags, tag)
				}
			}
		}
		cases = append(cases, item)
	}
	return cases, filepath.Dir(absolute), nil
}

func evaluate(ctx context.Context, expectedPath string, installer *modelsetup.Installer) (evaluationReport, error) {
	if strings.TrimSpace(expectedPath) == "" {
		return evaluationReport{}, fmt.Errorf("--expected CSV is required")
	}
	cases, root, err := readEvaluationCases(expectedPath)
	if err != nil {
		return evaluationReport{}, err
	}
	status, err := installer.Status()
	if err != nil {
		return evaluationReport{}, err
	}
	if !status.Ready {
		return evaluationReport{}, fmt.Errorf("BGE model is not ready; run setup before evaluate")
	}
	reranker, err := scoring.NewBGEReranker(scoring.BGEConfig{
		ModelID: installer.Manifest.ModelID, Revision: installer.Manifest.Revision,
		ModelDir: installer.Paths.ModelDir, Python: installer.Paths.Python, Runner: installer.Paths.Runner,
	})
	if err != nil {
		return evaluationReport{}, err
	}
	defer reranker.Close()
	db, err := storage.Open(ctx, ":memory:")
	if err != nil {
		return evaluationReport{}, err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return evaluationReport{}, err
	}
	service, err := app.NewService(db, extract.DefaultAdapter(), reranker, filesystem.Local{}, domain.DefaultClassificationPolicy())
	if err != nil {
		return evaluationReport{}, err
	}
	directories := make(map[string]string)
	for _, item := range cases {
		dir := filepath.Dir(item.Path)
		if existing, ok := directories[dir]; ok && existing != item.Label {
			return evaluationReport{}, fmt.Errorf("directory %q has conflicting labels %q and %q", dir, existing, item.Label)
		}
		directories[dir] = item.Label
	}
	keys := make([]string, 0, len(directories))
	for dir := range directories {
		keys = append(keys, dir)
	}
	sort.Strings(keys)
	labelsByID := make(map[string]string, len(keys))
	for _, dir := range keys {
		label := directories[dir]
		folder, registerErr := service.RegisterFolder(ctx, filepath.Join(root, dir), label, label+"に関する文書・資料", "")
		if registerErr != nil {
			return evaluationReport{}, fmt.Errorf("register evaluation folder %q: %w", dir, registerErr)
		}
		labelsByID[folder.ID] = label
	}
	report := evaluationReport{Model: installer.Manifest.ModelID + "@" + installer.Manifest.Revision, Total: len(cases)}
	for _, item := range cases {
		result, ingestErr := service.Ingest(ctx, filepath.Join(root, item.Path))
		if ingestErr != nil {
			return evaluationReport{}, fmt.Errorf("evaluate %q: %w", item.Path, ingestErr)
		}
		if result.File.ExtractStatus != "ok" {
			return evaluationReport{}, fmt.Errorf("extract %q: %s", item.Path, result.File.ExtractError)
		}
		for _, run := range result.ModelRuns {
			if run.Status != "ok" {
				return evaluationReport{}, fmt.Errorf("score %q: %s", item.Path, run.Error)
			}
		}
		row := evaluationRow{Path: item.Path, Expected: item.Label, Action: string(result.Classification.Action),
			Score: result.Classification.Top.Score, ExpectedTags: item.Tags, PredictedTags: result.Tags, TagsKnown: item.TagsKnown}
		row.TopCandidate = labelsByID[result.Classification.Top.FolderID]
		if result.Classification.Action != domain.ClassificationUnclassified {
			row.Predicted = labelsByID[result.Classification.Top.FolderID]
		}
		report.Cases = append(report.Cases, row)
	}
	return summarizeEvaluation(report), nil
}

func summarizeEvaluation(report evaluationReport) evaluationReport {
	byLabel := make(map[string]*evaluationLabel)
	get := func(label string) *evaluationLabel {
		if byLabel[label] == nil {
			byLabel[label] = &evaluationLabel{Label: label}
		}
		return byLabel[label]
	}
	tagTP, tagFP, tagFN, exactTags := 0, 0, 0, 0
	for _, row := range report.Cases {
		if row.Predicted == row.Expected {
			report.Correct++
			get(row.Expected).TP++
		} else {
			get(row.Expected).FN++
			if row.Predicted != "" {
				get(row.Predicted).FP++
			}
		}
		if row.Predicted == "" {
			report.Abstained++
		}
		if !row.TagsKnown {
			continue
		}
		report.TagSamples++
		expected := make(map[string]bool)
		predicted := make(map[string]bool)
		for _, tag := range row.ExpectedTags {
			expected[tag] = true
		}
		for _, tag := range row.PredictedTags {
			predicted[tag] = true
		}
		if sameTags(expected, predicted) {
			exactTags++
		}
		for tag := range predicted {
			if expected[tag] {
				tagTP++
			} else {
				tagFP++
			}
		}
		for tag := range expected {
			if !predicted[tag] {
				tagFN++
			}
		}
	}
	if report.Total > 0 {
		report.Accuracy = float64(report.Correct) / float64(report.Total)
		report.Coverage = float64(report.Total-report.Abstained) / float64(report.Total)
	}
	if report.TagSamples > 0 {
		report.TagExact = float64(exactTags) / float64(report.TagSamples)
	}
	report.TagPrecision = ratio(tagTP, tagTP+tagFP)
	report.TagRecall = ratio(tagTP, tagTP+tagFN)
	report.TagF1 = harmonic(report.TagPrecision, report.TagRecall)
	for _, label := range byLabel {
		label.Precision = ratio(label.TP, label.TP+label.FP)
		label.Recall = ratio(label.TP, label.TP+label.FN)
		label.F1 = harmonic(label.Precision, label.Recall)
		report.Labels = append(report.Labels, *label)
	}
	sort.Slice(report.Labels, func(i, j int) bool { return report.Labels[i].Label < report.Labels[j].Label })
	return report
}

func sameTags(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for tag := range a {
		if !b[tag] {
			return false
		}
	}
	return true
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func harmonic(precision, recall float64) float64 {
	if precision+recall == 0 {
		return 0
	}
	return 2 * precision * recall / (precision + recall)
}
