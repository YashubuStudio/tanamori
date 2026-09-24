package app

import (
	"strings"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

var tagKeywords = map[string][]string{
	"会計":     {"会計", "請求", "経費", "売上", "入金", "精算"},
	"会議":     {"会議", "議事録", "定例", "アジェンダ", "ミーティング", "打合せ"},
	"ホームページ": {"ホームページ", "ウェブサイト", "webサイト", "サイト制作"},
	"修正依頼":   {"修正依頼", "修正", "変更依頼"},
}

// Document tags are evidence about content, not destination folders.
func documentTags(name, body string, folders []domain.Folder) []string {
	content := strings.ToLower(name + "\n" + body)
	tags := make([]string, 0, len(domain.PriorityTags))
	for _, tag := range domain.PriorityTags {
		if tag == "顧客名" {
			for _, folder := range folders {
				if strings.HasSuffix(folder.Name, "様") && strings.Contains(content, strings.ToLower(folder.Name)) {
					tags = append(tags, tag)
					break
				}
			}
			continue
		}
		for _, keyword := range tagKeywords[tag] {
			if strings.Contains(content, strings.ToLower(keyword)) {
				tags = append(tags, tag)
				break
			}
		}
	}
	return tags
}

func folderTag(name string) string {
	if strings.HasSuffix(name, "様") {
		return "顧客名"
	}
	for _, tag := range []string{"会計", "会議", "ホームページ", "修正依頼"} {
		if strings.Contains(name, tag) {
			return tag
		}
	}
	return ""
}

func tagPriority(folder domain.Folder, tags []string, settings domain.Settings) int {
	tag := folderTag(folder.Name)
	for _, found := range tags {
		if tag == found {
			return settings.Priorities[tag]
		}
	}
	return 0
}
