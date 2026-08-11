// Package app contains dagmar's application services: orchestration that wires the Dagger
// SDK (Tier A, used DIRECTLY — ADR-0001; ADR-0010 §3) to the pure domain core. app is the
// boundary the main Dagger object (Dagmar) delegates to. Dagger-direct logic here is
// integration-tested via a real engine (//go:build integration).
package app

import (
	"context"
	"fmt"

	"dagger/dagmar/internal/dagger"
)

// Code is dagmar's coder-loop (Phase 2 cognition, ADR-0021 D2). It builds the Dagger Env,
// constructs the LLM with prompt + budget, drives the Loop, and returns the modified workspace
// Directory.
//
// The Env is constructed with:
//   - Privileged=true: gives the LLM access to the core Dagger API (Directory, File, Container,
//     etc.) — without this the agent only sees DeclareOutput + ReadLogs and cannot read/write files.
//   - Writable=true: allows the LLM to declare and save outputs (the modified workspace).
//   - DirectoryInput("source", ...): the project source the agent reads from.
//   - DirectoryOutput("result", ...): where the agent saves the modified source via the Save tool.
//   - WithMainModule(projectMod): registers the project module's LLM-Tool hooks (ADR-0019)
//     when moduleRef is non-empty.
//
// After the Loop, the "result" output binding holds the agent's modified workspace.
//
// The prompt file is pre-composed by the controller (ADR-0005 cross-store merge) — this
// function does NOT compose prompts. It receives a ready .md file for WithPromptFile.
//
// MaxAPICalls is the engine-enforced token/cost budget (v0.21.8 LLMOpts.MaxAPICalls).
// When exhausted, the Loop terminates and the Run fails (gate RED).
//
// Tier A is used directly here (no port); this path is integration-tested.
func Code(
	ctx context.Context,
	source *dagger.Directory,
	promptFile *dagger.File,
	model string,
	maxAPICalls int,
	// moduleRef is the project module reference (e.g. "github.com/denkhaus/dagmar/.dagmar"
	// for remote dogfooding). Loaded via ModuleSource. When empty, no project module is
	// loaded — the agent has core API tools only (no LLM-Tool hooks).
	moduleRef string,
) (*dagger.Directory, error) {
	client := dagger.Connect()

	// 1. Build the Env: privileged + writable + input/output directory bindings.
	//    The agent reads from "source", modifies via Directory.withNewFile etc.,
	//    and saves the result via the Save tool to "result".
	env := client.Env(dagger.EnvOpts{
		Privileged: true,
		Writable:   true,
	}).
		WithDirectoryInput("source", source, "The project source directory").
		WithDirectoryOutput("result", "The modified source directory after the agent's work")

	// 2. Optionally load the project module so its functions become LLM tools (ADR-0019).
	var projectMod *dagger.Module
	if moduleRef != "" {
		var err error
		projectMod, err = client.ModuleSource(moduleRef).AsModule().Sync(ctx)
		if err != nil {
			return nil, fmt.Errorf("code: load project module %q: %w", moduleRef, err)
		}
		env = env.WithMainModule(projectMod)
	}

	// 3. Build the LLM with prompt + budget (ADR-0021 D4).
	llm := client.LLM(dagger.LLMOpts{
		Model:       model,
		MaxAPICalls: maxAPICalls,
	}).
		WithEnv(env).
		WithPromptFile(promptFile)

	// 4. Apply per-role tool-surface policy (ADR-0024): block dagmar-bootstrap,
	// dagmar-gate (deterministic controller steps), keep Container (go build).
	llm = blockForRole(ctx, llm, projectMod, RoleCoder)

	// 5. Block on the Loop — the agent works until done or budget exhausted.
	llm = llm.Loop()
	if _, err := llm.Sync(ctx); err != nil {
		return nil, fmt.Errorf("code: loop failed: %w", err)
	}

	// 5. Token usage (cost accounting — captured here, not by the controller).
	_, _ = llm.TokenUsage().TotalTokens(ctx)

	// 6. Read the "result" output binding — the agent's modified workspace.
	//    The controller dispatches Diff() to extract the diff for the PR flow
	//    (ADR-0021 D8, ADR-0020 D3).
	result := env.Output("result").AsDirectory()
	return result, nil
}

// Diff computes the difference between a pre-Loop and post-Loop workspace (ADR-0021 D8).
// Returns a Directory containing only the changed files (v0.21.8: Directory.Diff returns
// *Directory, not *Changeset). The controller calls this after Code() to extract the
// agent's changes for the PR flow.
func Diff(after, before *dagger.Directory) *dagger.Directory {
	return after.Diff(before)
}
