package prompts

import _ "embed"

//go:embed coder-meta.md
var CoderMetaPrompt string

//go:embed reviewer-meta.md
var ReviewerMetaPrompt string

//go:embed adjudicator-meta.md
var AdjudicatorMetaPrompt string

func MetaPromptForPhase(phase string) string {
	switch phase {
	case "pre-code":
		return CoderMetaPrompt
	case "pre-review":
		return ReviewerMetaPrompt
	default:
		return ""
	}
}
