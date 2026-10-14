package app

import (
	"context"
	"reflect"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

type testSemanticScorer struct{}

func (testSemanticScorer) SupportsSemanticTags() bool { return true }
func (testSemanticScorer) Score(context.Context, ScoreRequest) (ScoreResult, error) {
	return ScoreResult{}, nil
}

type testFallbackScorer struct{}

func (testFallbackScorer) Score(context.Context, ScoreRequest) (ScoreResult, error) {
	return ScoreResult{}, nil
}

func TestCandidateTagsFromExistingSemanticFolderScores(t *testing.T) {
	folders := []domain.Folder{
		{ID: "meeting", Name: "会議"}, {ID: "website", Name: "ホームページ"},
		{ID: "customer", Name: "鈴木様"}, {ID: "revision", Name: "修正依頼"},
	}
	candidates := []domain.CandidateScore{
		{FolderID: "website", Score: 0.504},
		{FolderID: "meeting", Score: 0.451},
		{FolderID: "customer", Score: 0.409},
		{FolderID: "revision", Score: 0.255},
	}
	selection := candidateTags(testSemanticScorer{}, "制作定例アジェンダ.md", folders, candidates)
	tags := selection.Tags
	if want := []string{"会議", "ホームページ"}; !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	settings := domain.DefaultSettings()
	if tagPriority(folders[0], tags, settings) <= tagPriority(folders[1], tags, settings) {
		t.Fatal("meeting priority should be higher than website priority")
	}
}

func TestHighScoresRequireCloserMatch(t *testing.T) {
	folders := []domain.Folder{
		{ID: "accounting", Name: "会計"}, {ID: "customer", Name: "鈴木様"},
		{ID: "other-customer", Name: "斎藤様"}, {ID: "website", Name: "ホームページ"},
		{ID: "revision", Name: "修正依頼"},
	}
	candidates := []domain.CandidateScore{
		{FolderID: "website", Score: 0.938}, {FolderID: "revision", Score: 0.929},
		{FolderID: "customer", Score: 0.918}, {FolderID: "accounting", Score: 0.882},
		{FolderID: "other-customer", Score: 0.709},
	}
	selection := candidateTags(testSemanticScorer{}, "鈴木様_初回相談メモ.txt", folders, candidates)
	if want := []string{"顧客名"}; !reflect.DeepEqual(selection.Tags, want) {
		t.Fatalf("tags = %v, want %v", selection.Tags, want)
	}
	if !selection.FolderIDs["customer"] || selection.FolderIDs["other-customer"] || selection.FolderIDs["accounting"] {
		t.Fatalf("priority assigned to unrelated folders: %v", selection.FolderIDs)
	}
	if got, want := candidateTags(testSemanticScorer{}, "週次ミーティング議事録.txt", folders, candidates).Tags, []string{"ホームページ"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("high-confidence winner tags = %v, want %v", got, want)
	}
}

func TestHighConfidenceMeetingStaysAheadOfCloseAccounting(t *testing.T) {
	folders := []domain.Folder{{ID: "accounting", Name: "会計"}, {ID: "meeting", Name: "会議"}}
	candidates := []domain.CandidateScore{{FolderID: "accounting", Score: 0.974}, {FolderID: "meeting", Score: 0.983}}
	selection := candidateTags(testSemanticScorer{}, "週次ミーティング議事録.txt", folders, candidates)
	if want := []string{"会議"}; !reflect.DeepEqual(selection.Tags, want) {
		t.Fatalf("tags = %v, want %v", selection.Tags, want)
	}
	if !selection.FolderIDs["meeting"] || selection.FolderIDs["accounting"] {
		t.Fatalf("unexpected priority targets: %v", selection.FolderIDs)
	}
}

func TestKeywordFallbackDoesNotInventRelatedTags(t *testing.T) {
	folders := []domain.Folder{{ID: "meeting", Name: "会議"}}
	candidates := []domain.CandidateScore{{FolderID: "meeting", Score: 1}}
	if tags := candidateTags(testFallbackScorer{}, "会議.txt", folders, candidates).Tags; len(tags) != 0 {
		t.Fatalf("fallback must not infer related tags: %v", tags)
	}
}
