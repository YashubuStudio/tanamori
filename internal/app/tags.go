package app

import (
	"strings"

	"github.com/YashubuStudio/tanamori/internal/domain"
)

// The keyword fallback deliberately does not claim to infer related tags.
type semanticTagScorer interface {
	SupportsSemanticTags() bool
}

type tagSelection struct {
	Tags      []string
	FolderIDs map[string]bool
}

// candidateTags uses scores the semantic model already calculated for managed
// folders. The labels come from the registered folder taxonomy; no synonym or
// keyword list is supplied to the model. Scores are relevance, not probability.
func candidateTags(scorer Scorer, fileName string, folders []domain.Folder, candidates []domain.CandidateScore) tagSelection {
	semantic, ok := scorer.(semanticTagScorer)
	if !ok || !semantic.SupportsSemanticTags() || len(candidates) == 0 {
		return tagSelection{}
	}
	best := candidates[0]
	for _, candidate := range candidates {
		if candidate.Score > best.Score {
			best = candidate
		}
	}
	if best.Score < 0.35 {
		return tagSelection{}
	}
	folderByID := make(map[string]domain.Folder, len(folders))
	for _, folder := range folders {
		folderByID[folder.ID] = folder
	}
	selection := tagSelection{FolderIDs: make(map[string]bool)}
	matched := make(map[string]bool, len(domain.PriorityTags))
	add := func(candidate domain.CandidateScore) {
		if tag := folderTag(folderByID[candidate.FolderID].Name); tag != "" {
			selection.FolderIDs[candidate.FolderID] = true
			matched[tag] = true
		}
	}
	if best.Score >= 0.8 {
		// At high relevance, an almost-tied higher-priority folder can still be
		// unrelated. Prefer the model winner unless a registered folder name is
		// explicitly present in the file name and its score is very close.
		explicit := false
		for _, candidate := range candidates {
			folder := folderByID[candidate.FolderID]
			if candidate.Score >= best.Score-0.025 && folder.Name != "" &&
				strings.Contains(strings.ToLower(fileName), strings.ToLower(folder.Name)) {
				add(candidate)
				explicit = true
			}
		}
		if !explicit {
			add(best)
		}
	} else {
		window := 0.06
		if best.Score >= 0.6 {
			window = 0.04
		}
		for _, candidate := range candidates {
			if candidate.Score >= best.Score-window {
				add(candidate)
			}
		}
	}
	selection.Tags = make([]string, 0, len(matched))
	for _, tag := range domain.PriorityTags {
		if matched[tag] {
			selection.Tags = append(selection.Tags, tag)
		}
	}
	return selection
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
