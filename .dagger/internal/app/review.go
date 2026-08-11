// Package app contains dagmar's application services.
//
// review.go is dagmar's reviewer-loop (ADR-0024 D4). The reviewer reads the coder's
// workspace, applies review criteria from the prompt, and outputs a structured JSON
// verdict (approve/veto + rationale) via WithJSONValueOutput.
//
// Unlike Code(), the reviewer:
//   - Does NOT use Writable=true — it reads source but never modifies it.
//   - Has NO DirectoryOutput — it outputs a JSON verdict, not a Directory.
//   - Uses WithJSONValueOutput for structured output (ADR-0025: no termination-log
//     for >2000-byte payloads; structured JSON enables future DSL pipelines).
//   - Returns a parsed ReviewVerdict struct, not a raw string.
//
// The output is extracted via extractJSONOutput with retry (ADR-0025 D2): if the agent
// doesn't set the output or the JSON doesn't validate, the agent is re-prompted.
package app

import (
	"context"
	"fmt"

	"dagger/dagmar/internal/dagger"
)

// ReviewVerdict is the structured output of the reviewer LLM loop (ADR-0024 D4).
// The JSON schema the agent must produce:
//
//	{
//	  "decision": "approve" | "veto",
//	  "rationale": "human-readable explanation",
//	  "issues": ["issue 1", "issue 2"]  // empty if approved
//	}
type ReviewVerdict struct {
	Decision  string   `json:"decision"`         // "approve" or "veto"
	Rationale string   `json:"rationale"`        // human-readable explanation
	Issues    []string `json:"issues,omitempty"` // specific issues (if veto)
}

// IsApprove returns true if the verdict is an approval.
func (v ReviewVerdict) IsApprove() bool {
	return v.Decision == "approve"
}

// reviewCorrectionPrompt is the retry prompt when the agent didn't set valid JSON output.
const reviewCorrectionPrompt = `Your previous response did not produce a valid JSON output in the "verdict" binding.
You MUST use the Save tool to set the "verdict" output to a JSON object with this exact schema:

{
  "decision": "approve" or "veto",
  "rationale": "brief explanation of your decision",
  "issues": ["list of specific issues if vetoing, omit if approving"]
}

Your last output was: %.200s

Please save your verdict now using the Save tool.`

// Review is dagmar's reviewer-loop (ADR-0024 D4). It builds a read-only Env,
// drives the LLM Loop, and extracts the structured verdict via extractJSONOutput.
func Review(
	ctx context.Context,
	source *dagger.Directory,
	promptFile *dagger.File,
	model string,
	maxAPICalls int,
	moduleRef string,
) (ReviewVerdict, string, error) {
	client := dagger.Connect()

	// 1. Build the Env: privileged (tool access) but NOT writable (read-only).
	env := client.Env(dagger.EnvOpts{
		Privileged: true,
	}).
		WithDirectoryInput("source", source, "The workspace to review (read-only)").
		WithJSONValueOutput("verdict", `JSON: {"decision":"approve|veto","rationale":"explanation","issues":["..."]}`)

	// 2. Optionally load the project module for dagmar-issues + dagmar-memory hooks.
	if moduleRef != "" {
		projectMod, err := client.ModuleSource(moduleRef).AsModule().Sync(ctx)
		if err != nil {
			return ReviewVerdict{}, "", fmt.Errorf("review: load project module %q: %w", moduleRef, err)
		}
		env = env.WithMainModule(projectMod)
	}

	// 3. Build the LLM with prompt + budget.
	llm := client.LLM(dagger.LLMOpts{
		Model:       model,
		MaxAPICalls: maxAPICalls,
	}).
		WithEnv(env).
		WithPromptFile(promptFile)

	// 4. Apply per-role tool-surface policy (ADR-0024): block gate, bootstrap, container.
	llm = blockForRole(llm, RoleReviewer)

	// 5. Drive the Loop.
	llm = llm.Loop()
	if _, err := llm.Sync(ctx); err != nil {
		return ReviewVerdict{}, "", fmt.Errorf("review: loop failed: %w", err)
	}

	// 6. Extract the structured verdict with retry (ADR-0025).
	var verdict ReviewVerdict
	rawJSON, err := extractJSONOutput(ctx, llm, env, "verdict", &verdict, reviewCorrectionPrompt)
	if err != nil {
		return ReviewVerdict{}, rawJSON, fmt.Errorf("review: %w", err)
	}

	return verdict, rawJSON, nil
}
