package domain

import "testing"

func TestClassificationPolicy(t *testing.T) {
	policy := DefaultClassificationPolicy()
	policy.AutoEnabled = true

	auto := policy.Evaluate([]CandidateScore{{FolderID: "a", Score: 0.94}, {FolderID: "b", Score: 0.70}})
	if auto.Action != ClassificationAuto || auto.Top.FolderID != "a" {
		t.Fatalf("unexpected auto result: %#v", auto)
	}

	proposal := policy.Evaluate([]CandidateScore{{FolderID: "a", Score: 0.80}, {FolderID: "b", Score: 0.73}})
	if proposal.Action != ClassificationProposal {
		t.Fatalf("unexpected proposal result: %#v", proposal)
	}

	unclassified := policy.Evaluate([]CandidateScore{{FolderID: "a", Score: 0.40}})
	if unclassified.Action != ClassificationUnclassified {
		t.Fatalf("unexpected unclassified result: %#v", unclassified)
	}
}

func TestProposalTransitions(t *testing.T) {
	if !CanTransitionProposal(ProposalPending, ProposalAccepted) {
		t.Fatal("pending should transition to accepted")
	}
	if CanTransitionProposal(ProposalApplied, ProposalPending) {
		t.Fatal("applied proposal must be terminal")
	}
}
