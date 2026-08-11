// Package app contains dagmar's application services: orchestration that wires the Dagger
// SDK (Tier A, used DIRECTLY — ADR-0001; ADR-0010 §3) to the pure domain core. app is the
// boundary the main Dagger object (Dagmar) delegates to. Dagger-direct logic here is
// integration-tested via a real engine (//go:build integration).
package app

import (
	"context"
	"fmt"

	_ "embed"

	"dagger/dagmar/internal/dagger"
)

//go:embed coder-meta.md
var coderMetaPrompt string

//go:embed reviewer-meta.md
var reviewerMetaPrompt string

// metaPromptForPhase returns the meta-prompt for the given pipeline phase.
func metaPromptForPhase(phase string) string {
	switch phase {
	case "pre-code":
		return coderMetaPrompt
	case "pre-review":
		return reviewerMetaPrompt
	default:
		return ""
	}
}

// Prompt is dagmar's prompter-loop (ADR-0023 D1). It builds a read-only Env, constructs the LLM
// with the phase-appropriate meta-prompt + task context, drives the Loop, and returns the
// synthesized prompt as a string.
//
// Unlike Code() (ADR-0021 D2), the Prompter:
//   - Does NOT use Writable=true — it reads source but never modifies it.
//   - Has NO DirectoryOutput — the output is a string (the synthesized prompt), not a Directory.
//   - Uses WithPrompt (meta-prompt + task context) instead of WithPromptFile.
//   - Returns llm.LastReply(ctx) — the final synthesized prompt text.
//
// The Prompter is a full Loop(), not single-shot: it calls dagmar-issues and dagmar-memory as
// LLM-Tool hooks during synthesis, reads files from source, then produces the prompt.
//
// Env construction (ADR-0022):
//   - Privileged=true: gives the LLM access to the core Dagger API (Directory, File, etc.)
//     so the prompter can read project files.
//   - WithDirectoryInput("source", ...): read-only project source.
//   - WithMainModule(projectMod): registers dagmar-issues + dagmar-memory as LLM tools
//     when moduleRef is non-empty.
//
// MaxAPICalls is low (default 10) — prompt synthesis is well-bounded (ADR-0023 D1).
//
// Tier A is used directly here (no port); this path is integration-tested.
func Prompt(
	ctx context.Context,
	source *dagger.Directory,
	phase string,
	taskContext string,
	model string,
	maxAPICalls int,
	// moduleRef is the project module reference (e.g. ".dagmar" for dogfooding).
	// Loaded via ModuleSource so its functions (dagmar-issues, dagmar-memory) become
	// LLM-Tool hooks. When empty, the prompter has core API tools only.
	moduleRef string,
) (string, error) {
	// 1. Select the meta-prompt by phase (ADR-0023 D9).
	//    "pre-code" → coder-meta.md, "pre-review" → reviewer-meta.md.
	metaPrompt := metaPromptForPhase(phase)
	if metaPrompt == "" {
		return "", fmt.Errorf("prompt: unknown phase %q (want \"pre-code\" or \"pre-review\")", phase)
	}

	client := dagger.Connect()

	// 2. Build the Env: privileged (tool access) but NOT writable (read-only).
	//    The prompter reads source, issues, and memory — it writes nothing.
	env := client.Env(dagger.EnvOpts{
		Privileged: true,
	}).
		WithDirectoryInput("source", source, "Project source (read-only)")

	// 3. Optionally load the project module so dagmar-issues + dagmar-memory become
	//    LLM-Tool hooks (ADR-0019). The prompter calls these during synthesis.
	if moduleRef != "" {
		projectMod, err := client.ModuleSource(moduleRef).AsModule().Sync(ctx)
		if err != nil {
			return "", fmt.Errorf("prompt: load project module %q: %w", moduleRef, err)
		}
		env = env.WithMainModule(projectMod)
	}

	// 4. Build the LLM with the meta-prompt + task context + budget.
	//    The meta-prompt tells the LLM WHAT to synthesize and which mandatory rules
	//    to include. The task context is the issue text / task description.
	llm := client.LLM(dagger.LLMOpts{
		Model:       model,
		MaxAPICalls: maxAPICalls,
	}).
		WithEnv(env).
		WithPrompt(metaPrompt).
		WithPrompt(taskContext)

	// 5. Apply per-role tool-surface policy (ADR-0024): block dagmar-bootstrap,
	// dagmar-gate, and Container (prompter is read-only — no container exec).
	llm = blockForRole(llm, RolePrompter)

	// 6. Block on the Loop — the prompter reads files, calls tools, and synthesizes.
	llm = llm.Loop()
	if _, err := llm.Sync(ctx); err != nil {
		return "", fmt.Errorf("prompt: loop failed: %w", err)
	}

	// 6. The synthesized prompt is the LLM's last reply.
	return llm.LastReply(ctx)
}
