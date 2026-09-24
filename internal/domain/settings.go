package domain

import (
	"fmt"
	"strings"
)

var PriorityTags = []string{"会計", "会議", "顧客名", "ホームページ", "修正依頼"}

type Settings struct {
	Priorities    map[string]int `json:"priorities"`
	RenamePattern string         `json:"rename_pattern"`
}

func DefaultSettings() Settings {
	return Settings{Priorities: map[string]int{"会計": 100, "会議": 80, "顧客名": 60, "ホームページ": 40, "修正依頼": 40}, RenamePattern: "{name} ({n}){ext}"}
}

func (s Settings) Validate() error {
	for _, tag := range PriorityTags {
		if _, ok := s.Priorities[tag]; !ok {
			return fmt.Errorf("priority for %s is required", tag)
		}
		if s.Priorities[tag] < 0 || s.Priorities[tag] > 1000 {
			return fmt.Errorf("priority for %s must be 0..1000", tag)
		}
	}
	if len(s.RenamePattern) > 200 || !strings.Contains(s.RenamePattern, "{n}") {
		return fmt.Errorf("rename pattern must include {n} and be at most 200 characters")
	}
	if strings.ContainsAny(s.RenamePattern, `\/:*?"<>|`) || strings.HasSuffix(s.RenamePattern, ".") || strings.HasSuffix(s.RenamePattern, " ") {
		return fmt.Errorf("rename pattern contains invalid Windows filename characters")
	}
	return nil
}
