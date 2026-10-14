package main

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestSummarizeEvaluation(t *testing.T) {
	report := summarizeEvaluation(evaluationReport{Total: 3, Cases: []evaluationRow{
		{Expected: "A", Predicted: "A", ExpectedTags: []string{"会計"}, PredictedTags: []string{"会計"}, TagsKnown: true},
		{Expected: "A", Predicted: "B", ExpectedTags: []string{"会計"}, PredictedTags: []string{"会議"}, TagsKnown: true},
		{Expected: "B", TagsKnown: true},
	}})
	if report.Correct != 1 || report.Abstained != 1 || len(report.Labels) != 2 {
		t.Fatalf("wrong classification counts: %+v", report)
	}
	if math.Abs(report.Accuracy-1.0/3) > 1e-9 || math.Abs(report.Coverage-2.0/3) > 1e-9 {
		t.Fatalf("wrong classification ratios: %+v", report)
	}
	if report.Labels[0].TP != 1 || report.Labels[0].FN != 1 || report.Labels[1].FP != 1 {
		t.Fatalf("wrong per-label counts: %+v", report.Labels)
	}
	if report.TagPrecision != 0.5 || report.TagRecall != 0.5 || math.Abs(report.TagExact-2.0/3) > 1e-9 {
		t.Fatalf("wrong tag metrics: %+v", report)
	}
}

func TestReadEvaluationCasesRejectsTraversalAndDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "expected.csv")
	for _, body := range []string{
		"relative_path,expected_label\n../outside.txt,A\n",
		"relative_path,expected_label\nA/one.txt,A\nA/one.txt,A\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readEvaluationCases(path); err == nil {
			t.Fatalf("accepted invalid CSV: %q", body)
		}
	}
}
