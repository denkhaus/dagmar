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
	"fmt"

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
func blockForRole(llm *dagger.LLM, role Role) *dagger.LLM {
	// Every LLM role: block infrastructure + gate functions.
	// dagmar-bootstrap and dagmar-gate are never LLM tools — they are deterministic
	// controller-dispatched steps.
	llm = llm.WithBlockedFunction("Dagmar", "dagmarBootstrap").
		WithBlockedFunction("Dagmar", "dagmarGate")

	// Read-only roles: block Container entirely (no WithExec, no network exec).
	// The coder keeps Container — it needs go build.
	if role != RoleCoder {
		llm = llm.WithBlockedFunction("Container", "withExec")
	}

	return llm
}

// validateRole returns an error if the role is unrecognized.
func validateRole(role Role) error {
	switch role {
	case RolePrompter, RoleCoder, RoleReviewer, RoleAdjudicator:
		return nil
	default:
		return fmt.Errorf("unknown role %q (want prompter|coder|reviewer|adjudicator)", role)
	}
}
