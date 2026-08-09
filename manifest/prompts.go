// prompts.go — Shared meta-prompts, embedded via go:embed (dagmar-481f).
//
// The meta-prompt Markdown files live HERE (manifest/prompts/) and are embedded into the
// Go binary. All modules that need meta-prompts import from here — no duplication.

package manifest

import _ "embed"

// CoderMetaPrompt is the meta-prompt for the pre-code phase. It instructs the Prompter-LLM
// to synthesize a coding prompt, including mandatory safety, workflow, and output-format rules.
//
//go:embed prompts/coder-meta.md
var CoderMetaPrompt string

// ReviewerMetaPrompt is the meta-prompt for the pre-review phase. It instructs the
// Prompter-LLM to synthesize a review prompt, including mandatory anti-gate-manipulation rules.
//
//go:embed prompts/reviewer-meta.md
var ReviewerMetaPrompt string

// AdjudicatorMetaPrompt is the meta-prompt for the adjudication phase. It guides the
// Adjudicator-LLM in resolving gate↔reviewer disagreement.
//
//go:embed prompts/adjudicator-meta.md
var AdjudicatorMetaPrompt string

// MetaPromptForPhase returns the meta-prompt for the given pipeline phase.
//
// Recognized phases:
//   - "pre-code":   coder meta-prompt
//   - "pre-review": reviewer meta-prompt
//
// The adjudicator meta-prompt is not returned here — callers reference AdjudicatorMetaPrompt
// directly.
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
