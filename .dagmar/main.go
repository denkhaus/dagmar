// Package main is dagmar-as-a-Project's conformance module (ADR-0014): it exposes the
// gate-family contract (dagmar-bootstrap + dagmar-gate) that the platform — and any caller —
// invokes by module-ref (`dagger call -m .dagmar dagmar-bootstrap | dagmar-gate --source .`).
//
// This module IS dagmar dogfooding itself AS a Project: the same conformance contract every
// conforming Project exposes (ADR-0009 §2 / ADR-0012 §4). It is deliberately separate from the
// platform module in .dagger/ (Up/DeployEngine/Probe*/Sandbox) so the platform addresses
// dagmar-as-a-Project by ref — the abstraction a normal Project uses — instead of calling its
// own gate functions directly (the shortcuit ADR-0014 closes by structure, not just convention).
package main

import (
	"context"

	"dagger/dagmar-project/internal/dagger"
	"dagger/dagmar-project/internal/workflows"
)

// Dagmar is dagmar-as-a-Project's main Dagger object (ADR-0014). It carries the Project source
// as bound state for the LLM-Tool hooks (dagmar-issues, dagmar-memory), which need it to build
// containers with the workspace mounted. The gate-family methods still take source explicitly.
type Dagmar struct {
	// Source is the project source directory, resolved from dag.CurrentModule().Source()
	// when the module functions are called by the LLM via WithMainModule.
	// +private
	Source *dagger.Directory
}

// DagmarBootstrap is dagmar's gate-family PREPARE step (ADR-0009 §2 / ADR-0012 §4): an
// always-Dagger function that rolls out the Project's mise toolchain (mise.toml) into the gate
// container before verification. Run once per workspace — by CI, lefthook pre-push, or the agent
// setup. The method name exposes as the dagger function `dagmar-bootstrap` (the
// conformance-contract name; kebab-cased by the Go SDK). Delegates to workflows.
func (m *Dagmar) DagmarBootstrap(
	ctx context.Context,
	// source is the Project's source tree (CI contract: `dagger call -m .dagmar dagmar-bootstrap --source .`).
	source *dagger.Directory,
	// githubToken authenticates ubi GitHub-API calls (avoids rate-limiting). Optional: nil = unauthenticated.
	// Pass via `--github-token env:GITHUB_TOKEN` (dagger reads from the local environment as a Secret).
	// +optional
	githubToken *dagger.Secret,
) (string, error) {
	return workflows.Bootstrap(ctx, source, githubToken)
}

// DagmarGate is dagmar's gate-family VERIFY step (ADR-0009 §2 / ADR-0012 §4 / ADR-0017 §3):
// an always-Dagger function with hard-coded Go checkables (NOT YAML manifest). The check logic —
// build, vet, test, gofmt, secret scan, coverage — lives entirely in Go code inside the gate
// function. Reused in CI (`dagger call -m .dagmar dagmar-gate --source .`) AND in-loop (coder
// self-verification, Phase 2). Exposes as the dagger function `dagmar-gate`. Delegates to workflows.
func (m *Dagmar) DagmarGate(
	ctx context.Context,
	// source is the Project's source tree (CI contract: `dagger call -m .dagmar dagmar-gate --source .`).
	source *dagger.Directory,
	// githubToken authenticates ubi GitHub-API calls (avoids rate-limiting). Optional: nil = unauthenticated.
	// Pass via `--github-token env:GITHUB_TOKEN` (dagger reads from the local environment as a Secret).
	// +optional
	githubToken *dagger.Secret,
	// coverageFloorBps is the ratcheted coverage floor in basis points (0–10000, e.g. 7850 = 78.50%).
	// When > 0, the gate measures total go test coverage and fails if below the floor (dagmar-4154).
	// 0 = coverage check disabled.
	// +optional
	// +default=0
	coverageFloorBps int,
) (string, error) {
	return workflows.Gate(ctx, source, githubToken, coverageFloorBps)
}

// DagmarIssues is dagmar's issue-tracker LLM-Tool hook (ADR-0019 D2 / ADR-0017 §2).
// Registered via Env.WithMainModule() and called by the LLM as a native Dagger tool during the
// loop. The LLM uses it to read, search, create, and update issues in the project's tracker
// (seeds). Delegates to workflows.DagmarIssues.
//
// Operates on the project worktree implicitly (ADR-0019 D2) — no source parameter. The backing
// CLI (sd) reads .seeds/ from the current working directory.
func (m *Dagmar) DagmarIssues(
	ctx context.Context,
	// action: "read" | "search" | "create" | "update"
	action string,
	// id: issue identifier (for read/update).
	// +optional
	id string,
	// query: search text (for search).
	// +optional
	query string,
	// title: issue title (for create).
	// +optional
	title string,
	// body: issue body (for create/update).
	// +optional
	body string,
) (string, error) {
	src := m.Source
	if src == nil {
		src = dag.CurrentModule().Source()
	}
	return workflows.DagmarIssues(ctx, src, action, id, query, title, body)
}

// DagmarMemory is dagmar's expertise-store LLM-Tool hook (ADR-0019 D2 / ADR-0017 §2).
// Registered via Env.WithMainModule() and called by the LLM as a native Dagger tool during the
// loop. The LLM uses it to read, search, and write project expertise (mulch). Delegates to
// workflows.DagmarMemory.
//
// Operates on the project worktree implicitly (ADR-0019 D2) — no source parameter. The backing
// CLI (ml) reads .mulch/ from the current working directory.
func (m *Dagmar) DagmarMemory(
	ctx context.Context,
	// action: "read" | "search" | "write"
	action string,
	// query: expertise query or domain (for read/search).
	// +optional
	query string,
	// key: record key/domain (for write).
	// +optional
	key string,
	// value: expertise content (for write).
	// +optional
	value string,
) (string, error) {
	src := m.Source
	if src == nil {
		src = dag.CurrentModule().Source()
	}
	return workflows.DagmarMemory(ctx, src, action, query, key, value)
}
