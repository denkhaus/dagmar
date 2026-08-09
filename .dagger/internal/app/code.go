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

// Code is dagmar's coder-loop (Phase 2 cognition, ADR-0021 D2). It builds the Dagger Env
// (workspace + project module's LLM-Tool hooks), constructs the LLM with prompt + budget,
// drives the Loop, and returns the modified workspace Directory.
//
// The Env is constructed with WithMainModule(projectModule) — this registers the PROJECT
// module's functions (dagmar-issues/dagmar-memory/dagmar-prompt, ADR-0019) as LLM tools.
// The project module is loaded from moduleRef (the Project CR's moduleRef, ADR-0014).
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
	// moduleRef is the project module reference (e.g. ".dagmar" for dogfooding, or
	// "github.com/denkhaus/dagmar/.dagmar" for remote). Loaded via ModuleSource.
	moduleRef string,
) (*dagger.Directory, error) {
	// 1. Load the project module so its functions become LLM tools.
	//    ModuleSource(ref) → AsModule() → Sync() forces load + validation.
	projectMod, err := dagger.Connect().ModuleSource(moduleRef).AsModule().Sync(ctx)
	if err != nil {
		return nil, fmt.Errorf("code: load project module %q: %w", moduleRef, err)
	}

	// 2. Build the Env: workspace + project module's LLM-Tool hooks (ADR-0021 D2).
	env := dagger.Connect().Env().
		WithWorkspace(source).
		WithMainModule(projectMod)

	// 3. Build the LLM with prompt + budget (ADR-0021 D4).
	llm := dagger.Connect().LLM(dagger.LLMOpts{
		Model:       model,
		MaxAPICalls: maxAPICalls,
	}).
		WithEnv(env).
		WithPromptFile(promptFile)

	// 4. Block on the Loop — the agent works until done or budget exhausted.
	llm = llm.Loop()
	if _, err := llm.Sync(ctx); err != nil {
		return nil, fmt.Errorf("code: loop failed: %w", err)
	}

	// 5. Token usage (cost accounting — captured here, not by the controller).
	_, _ = llm.TokenUsage().TotalTokens(ctx)

	// 6. Return the modified workspace. The controller dispatches Diff()
	//    to extract the diff for the PR flow (ADR-0021 D8, ADR-0020 D3).
	return source, nil
}

// Diff computes the difference between a pre-Loop and post-Loop workspace (ADR-0021 D8).
// Returns a Directory containing only the changed files (v0.21.8: Directory.Diff returns
// *Directory, not *Changeset). The controller calls this after Code() to extract the
// agent's changes for the PR flow.
func Diff(after, before *dagger.Directory) *dagger.Directory {
	return after.Diff(before)
}
