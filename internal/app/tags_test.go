package app

import (
	"reflect"
	"testing"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

func TestAgendaTagsKeepMeetingAheadOfWebsite(t *testing.T) {
	folders := []domain.Folder{{ID: "meeting", Name: "会議"}, {ID: "website", Name: "ホームページ"}, {ID: "customer", Name: "鈴木様"}, {ID: "revision", Name: "修正依頼"}}
	text := "ホームページ案件のデザインレビュー。鈴木様から写真素材。修正依頼を担当者へ割り当てる。"
	tags := documentTags("制作定例アジェンダ.md", text, folders)
	want := []string{"会議", "顧客名", "ホームページ", "修正依頼"}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags = %v, want %v", tags, want)
	}
	settings := domain.DefaultSettings()
	if tagPriority(folders[0], tags, settings) <= tagPriority(folders[1], tags, settings) {
		t.Fatal("meeting should outrank website when both tags are present")
	}
	result := domain.DefaultClassificationPolicy().Evaluate([]domain.CandidateScore{
		{FolderID: "website", Score: 0.95, Priority: 40},
		{FolderID: "meeting", Score: 0.35, Priority: 80},
	})
	if result.Top.FolderID != "meeting" || result.Action != domain.ClassificationProposal {
		t.Fatalf("priority was ignored: %#v", result)
	}
}
