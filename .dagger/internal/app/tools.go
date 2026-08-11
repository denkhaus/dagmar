// Package app contains dagmar's application services.
//
// tools.go implements the per-role tool-surface policy (ADR-0024). Each LLM role
// receives a specific subset of the module's functions as LLM tools. The mechanism
// is Dagger's LLM.WithBlockedFunction(typeName, function) — a blacklist API that
// removes specific functions from the LLM's tool surface after WithMainModule has
// registered them all.
//
// WithMainModule registers ALL functions from the project module (.dagmar/) as tools.
// blockForRole then removes the ones the role must not access. This is the only
// tool-filtering mechanism available in Dagger v0.21.8 (no whitelist API exists).
package app

import (
	"context"

	"dagger/dagmar/internal/dagger"
)

// Role identifies an LLM agent role in the pipeline (ADR-0023 D6, ADR-0024 D1).
type Role string

const (
	RolePrompter    Role = "prompter"
	RoleCoder       Role = "coder"
	RoleReviewer    Role = "reviewer"
	RoleAdjudicator Role = "adjudicator"
)

// blockForRole removes functions from the LLM's tool surface that the role must not
// access (ADR-0024). WithMainModule registers all .dagmar functions (dagmar-bootstrap,
// dagmar-gate, dagmar-issues, dagmar-memory); this function blocks the inappropriate ones.
//
// Tool matrix (ADR-0024 D1):
//
//	Role         | dagmar-bootstrap | dagmar-gate | dagmar-issues | dagmar-memory | Container
//	-------------|------------------|-------------|---------------|---------------|----------
//	Prompter     | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//	Coder        | BLOCKED          | BLOCKED     | allowed       | allowed       | allowed (go build)
//	Reviewer     | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//	Adjudicator  | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//
// dagmar-bootstrap: infrastructure setup — never an LLM concern (Q2).
// dagmar-gate: deterministic post-loop step, controller-authority only (Q1). The coder
// gets gate feedback via the revise loop, not in-loop.
// Container: the read-only roles (prompter, reviewer, adjudicator) only read files;
// they must not execute containers (Q3). The coder needs Container for go build.
// blockForRole removes functions from the LLM's tool surface that the role must not
// access (ADR-0024). WithMainModule registers the project module's functions;
// this function blocks the inappropriate ones.
//
// Tool matrix (ADR-0024 D1):
//
//	Role         | dagmarBootstrap | dagmarGate | dagmarIssues | dagmarMemory | Container
//	-------------|------------------|-------------|---------------|---------------|----------
//	Prompter     | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//	Coder        | BLOCKED          | BLOCKED     | allowed       | allowed       | allowed (go build)
//	Reviewer     | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//	Adjudicator  | BLOCKED          | BLOCKED     | allowed       | allowed       | BLOCKED
//
// dagmar-bootstrap: infrastructure setup — never an LLM concern (Q2).
// dagmar-gate: deterministic post-loop step, controller-authority only (Q1). The coder
// gets gate feedback via the revise loop, not in-loop.
// Container: the read-only roles (prompter, reviewer, adjudicator) only read files;
// they must not execute containers (Q3). The coder needs Container for go build.
//
// CRITICAL: WithBlockedFunction is a lazy GraphQL query — if the function does not exist
// on the loaded module, the error fires at Loop/Sync time and breaks the entire LLM.
// We MUST introspect the module first and only block functions that actually exist.
// This handles both real projects (which expose dagmarBootstrap/dagmarGate) and
// dagmar-dogfooding (which loads the main module without those functions).
func blockForRole(ctx context.Context, llm *dagger.LLM, mod *dagger.Module, role Role) *dagger.LLM {
	// Collect function names to block per role.
	type blockEntry struct {
		typeName string
		fnName   string
	}
	var toBlock []blockEntry

	// Every LLM role: block infrastructure + gate functions if they exist.
	toBlock = append(toBlock,
		blockEntry{typeName: "Dagmar", fnName: "dagmarBootstrap"},
		blockEntry{typeName: "Dagmar", fnName: "dagmarGate"},
	)

	// Read-only roles: block Container entirely (no WithExec, no network exec).
	// The coder keeps Container — it needs go build.
	if role != RoleCoder {
		toBlock = append(toBlock, blockEntry{typeName: "Container", fnName: "withExec"})
	}

	// Introspect the loaded module to find which functions actually exist.
	// Only block functions that are present — WithBlockedFunction crashes on
	// non-existent functions (ADR-0024 D5, discovered during pipeline testing).
	available := availableModuleFunctions(ctx, mod)

	for _, b := range toBlock {
		if _, exists := available[b.typeName+"/"+b.fnName]; exists {
			llm = llm.WithBlockedFunction(b.typeName, b.fnName)
		}
	}

	return llm
}

// availableModuleFunctions introspects a loaded module and returns a set of
// "typeName/functionName" strings for all functions across all object types.
// Container.withExec is always included (it's a core type, not module-defined).
func availableModuleFunctions(ctx context.Context, mod *dagger.Module) map[string]bool {
	available := map[string]bool{
		// Container is a core Dagger type — always present.
		"Container/withExec": true,
	}

	if mod == nil {
		return available
	}

	objects, err := mod.Objects(ctx)
	if err != nil {
		return available
	}

	for _, td := range objects {
		objName, _ := td.AsObject().Name(ctx)
		funcs, err := td.AsObject().Functions(ctx)
		if err != nil {
			continue
		}
		for _, fn := range funcs {
			fnName, _ := fn.Name(ctx)
			available[objName+"/"+fnName] = true
		}
	}

	return available
}
