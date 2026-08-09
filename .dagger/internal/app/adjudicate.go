// Package app contains dagmar's application services: orchestration that wires the Dagger
// SDK (Tier A, used DIRECTLY — ADR-0001; ADR-0010 §3) to the pure domain core. app is the
// boundary the main Dagger object (Dagmar) delegates to. Dagger-direct logic here is
// integration-tested via a real engine (//go:build integration).
package app

import (
	"context"
	"fmt"

	"github.com/denkhaus/dagmar/manifest"

	"dagger/dagmar/internal/dagger"
)

// Adjudicate is dagmar's adjudicator-loop (ADR-0023 D4). When the deterministic Gate and
// the Reviewer-LLM disagree (gate green + reviewer veto, or gate red + reviewer approve),
// the Adjudicator resolves the conflict. It is the final automated decision maker before
// human escalation.
//
// Unlike Code() (ADR-0021 D2) and Prompt() (ADR-0023 D1), the Adjudicator:
//   - Does NOT use Writable=true — it reads source but never modifies it (read-only).
//   - Has NO DirectoryOutput — the output is a string (the structured verdict), not a Directory.
//   - Uses manifest.AdjudicatorMetaPrompt directly (NOT MetaPromptForPhase) — the Adjudicator
//     is not a standard pipeline phase selected by the phase parameter.
//   - Sends TWO WithPrompt calls: first the meta-prompt (role/instructions), then the
//     structured disagreement context (gate result, review result, original task).
//   - Returns llm.LastReply(ctx) — the structured verdict string.
//
// The Adjudicator is a full Loop(): it calls dagmar-issues and dagmar-memory as LLM-Tool
// hooks during analysis, reads files from source, then produces the verdict.
//
// Env construction (ADR-0022):
//   - Privileged=true: gives the LLM access to the core Dagger API (Directory, File, etc.)
//     so the adjudicator can read project files to investigate the disagreement.
//   - WithDirectoryInput("source", ...): read-only project source.
//   - WithMainModule(projectMod): registers dagmar-issues + dagmar-memory as LLM tools
//     when moduleRef is non-empty.
//
// Three resolution paths (ADR-0023 D4):
//   - reviewer-wrong: reviewer judgment incorrect → adjust reviewer context, proceed.
//   - gate-wrong: gate checkables too weak/strict → coder repairs the gate, full re-run.
//   - escalate: unresolvable → escalate to human.
//
// The Adjudicator always acts in the project's best interest. When it cannot, it escalates.
//
// Tier A is used directly here (no port); this path is integration-tested.
func Adjudicate(
	ctx context.Context,
	source *dagger.Directory,
	// gateResult is the gate's outcome: green/red and which checkables failed (if red).
	gateResult string,
	// reviewResult is the reviewer's outcome: approve/veto and the reviewer's rationale.
	reviewResult string,
	// taskContext is the original task / issue text the coder was asked to implement.
	taskContext string,
	model string,
	maxAPICalls int,
	// moduleRef is the project module reference (e.g. ".dagmar" for dogfooding).
	// Loaded via ModuleSource so its functions (dagmar-issues, dagmar-memory) become
	// LLM-Tool hooks. When empty, the adjudicator has core API tools only.
	moduleRef string,
) (string, error) {
	// 1. Select the meta-prompt directly (ADR-0023 D9).
	//    The Adjudicator is not a pipeline phase — its meta-prompt is referenced explicitly,
	//    not via MetaPromptForPhase.
	metaPrompt := manifest.AdjudicatorMetaPrompt

	// 2. Build the structured input context describing the disagreement.
	//    This is the second WithPrompt — the meta-prompt is sent first (role/instructions),
	//    then this carries the concrete gate/review/task data the Adjudicator analyzes.
	disagreementContext := fmt.Sprintf(
		"## Disagreement to resolve\n\n"+
			"### Gate result\n%s\n\n"+
			"### Review result\n%s\n\n"+
			"### Original task\n%s\n\n"+
			"Analyze the root cause and emit your verdict.",
		gateResult, reviewResult, taskContext,
	)

	client := dagger.Connect()

	// 3. Build the Env: privileged (tool access) but NOT writable (read-only).
	//    The Adjudicator reads source, issues, and memory — it writes nothing.
	env := client.Env(dagger.EnvOpts{
		Privileged: true,
	}).
		WithDirectoryInput("source", source, "Project source (read-only)")

	// 4. Optionally load the project module so dagmar-issues + dagmar-memory become
	//    LLM-Tool hooks (ADR-0019). The Adjudicator calls these during analysis.
	if moduleRef != "" {
		projectMod, err := client.ModuleSource(moduleRef).AsModule().Sync(ctx)
		if err != nil {
			return "", fmt.Errorf("adjudicate: load project module %q: %w", moduleRef, err)
		}
		env = env.WithMainModule(projectMod)
	}

	// 5. Build the LLM with the meta-prompt + disagreement context + budget.
	//    The meta-prompt tells the LLM its role and the three resolution paths.
	//    The disagreement context carries the concrete gate/review/task data.
	llm := client.LLM(dagger.LLMOpts{
		Model:       model,
		MaxAPICalls: maxAPICalls,
	}).
		WithEnv(env).
		WithPrompt(metaPrompt).
		WithPrompt(disagreementContext)

	// 6. Block on the Loop — the Adjudicator reads files, calls tools, and reasons.
	llm = llm.Loop()
	if _, err := llm.Sync(ctx); err != nil {
		return "", fmt.Errorf("adjudicate: loop failed: %w", err)
	}

	// 7. The structured verdict is the LLM's last reply.
	return llm.LastReply(ctx)
}
