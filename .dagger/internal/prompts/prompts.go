// Package prompts holds DAGMAR's static meta-prompts, embedded into the .dagger module
// binary via go:embed (ADR-0023 D9).
//
// The meta-prompts guide the Prompter-LLM: each file tells the prompter *what kind* of prompt
// to synthesize and which mandatory operational rules (safety, workflow, output format,
// anti-gate-manipulation) it must include. The prompter reads project context (source, issues,
// memory) at runtime and produces a tailored prompt for the specific task.
//
// These meta-prompts are DAGMAR-controlled constants. The project provides context, not direct
// prompt content. The Prompter-LLM is the gate that mediates project influence.
//
// NOTE: this package is a copy of the root module's internal/prompts. The root module's copy
// exists for the controller; this copy exists for the .dagger module. They are identical
// because Go's internal-package rule prevents cross-module imports, and Dagger codegen runs
// in a container that cannot resolve local replace directives outside .dagger/.
package prompts

import _ "embed"

// CoderMetaPrompt is the meta-prompt for the pre-code phase. It instructs the Prompter-LLM to
// synthesize a coding prompt for the Coder-LLM, including mandatory safety, workflow, and
// output-format rules.
//
//go:embed coder-meta.md
var CoderMetaPrompt string

// ReviewerMetaPrompt is the meta-prompt for the pre-review phase. It instructs the
// Prompter-LLM to synthesize a review prompt for the Reviewer-LLM, including mandatory
// anti-gate-manipulation rules and review criteria.
//
//go:embed reviewer-meta.md
var ReviewerMetaPrompt string

// AdjudicatorMetaPrompt is the meta-prompt for the adjudication phase. It guides the
// Adjudicator-LLM in resolving gate↔reviewer disagreement via one of three paths:
// calibrate the reviewer, instruct the coder to repair the gate, or escalate to a human.
//
//go:embed adjudicator-meta.md
var AdjudicatorMetaPrompt string

// MetaPromptForPhase returns the meta-prompt for the given pipeline phase.
//
// Recognized phases:
//   - "pre-code":       coder meta-prompt
//   - "pre-review":     reviewer meta-prompt
//
// The adjudicator meta-prompt is not returned here because adjudication is a distinct
// resolution step triggered by disagreement, not a standard pipeline phase selectable
// via the phase parameter. Callers that need it can reference AdjudicatorMetaPrompt directly.
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
