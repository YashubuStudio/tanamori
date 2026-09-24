package domain

import (
	"fmt"
	"sort"
)

type CandidateScore struct {
	FolderID string  `json:"folder_id"`
	Score    float64 `json:"score"`
	Priority int     `json:"priority,omitempty"`
	Reason   string  `json:"reason,omitempty"`
}

type ClassificationAction string

const (
	ClassificationAuto         ClassificationAction = "auto"
	ClassificationProposal     ClassificationAction = "proposal"
	ClassificationUnclassified ClassificationAction = "unclassified"
)

type ClassificationPolicy struct {
	AutoEnabled       bool    `json:"auto_enabled"`
	AutoThreshold     float64 `json:"auto_threshold"`
	ProposalThreshold float64 `json:"proposal_threshold"`
	MinimumMargin     float64 `json:"minimum_margin"`
}

func DefaultClassificationPolicy() ClassificationPolicy {
	return ClassificationPolicy{
		AutoEnabled:       false,
		AutoThreshold:     0.90,
		ProposalThreshold: 0.65,
		MinimumMargin:     0.15,
	}
}

func (policy ClassificationPolicy) Validate() error {
	for name, value := range map[string]float64{
		"auto threshold":     policy.AutoThreshold,
		"proposal threshold": policy.ProposalThreshold,
		"minimum margin":     policy.MinimumMargin,
	} {
		if value < 0 || value > 1 {
			return fmt.Errorf("%s must be between 0 and 1", name)
		}
	}
	if policy.AutoThreshold < policy.ProposalThreshold {
		return fmt.Errorf("auto threshold must not be lower than proposal threshold")
	}
	return nil
}

type ClassificationResult struct {
	Action     ClassificationAction `json:"action"`
	Candidates []CandidateScore     `json:"candidates,omitempty"`
	Top        CandidateScore       `json:"top,omitempty"`
	Margin     float64              `json:"margin"`
}

func (policy ClassificationPolicy) Evaluate(candidates []CandidateScore) ClassificationResult {
	sorted := append([]CandidateScore(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].Score > sorted[j].Score
	})
	result := ClassificationResult{Action: ClassificationUnclassified, Candidates: sorted}
	if len(sorted) == 0 {
		return result
	}
	result.Top = sorted[0]
	result.Margin = sorted[0].Score
	if len(sorted) > 1 {
		result.Margin = sorted[0].Score - sorted[1].Score
	}
	if policy.AutoEnabled && sorted[0].Score >= policy.AutoThreshold && result.Margin >= policy.MinimumMargin {
		result.Action = ClassificationAuto
		return result
	}
	if sorted[0].Priority > 0 || sorted[0].Score >= policy.ProposalThreshold {
		result.Action = ClassificationProposal
	}
	return result
}

func CanTransitionProposal(from, to ProposalState) bool {
	switch from {
	case ProposalPending:
		return to == ProposalAccepted || to == ProposalRejected || to == ProposalExpired
	case ProposalAccepted:
		return to == ProposalApplied || to == ProposalFailed
	default:
		return false
	}
}
