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

// Dagmar is dagmar-as-a-Project's main Dagger object (ADR-0014). It carries no per-Project
// bound state: the gate-family methods take the Project source per call. (Distinct from the
// platform module's Dagmar object in .dagger/, which binds a target Project + os-eco config;
// the inter-module name collision is benign — the two modules are path-addressed, -m .dagger
// vs -m .dagmar.) The dagger.json `name: "dagmar"` (not "dagmar-project") is forced: Dagger
// derives the main object type from `name` (kebab→CamelCase), so it must match `type Dagmar`;
// the go.mod path `dagger/dagmar-project` and the checkable name differ but do not collide.
type Dagmar struct{}

// DagmarBootstrap is dagmar's gate-family PREPARE step (ADR-0009 §2 / ADR-0012 §4): an
// always-Dagger function that rolls out the Project's mise toolchain (mise.toml) into the gate
// container before verification. Run once per workspace — by CI, lefthook pre-push, or the agent
// setup. The method name exposes as the dagger function `dagmar-bootstrap` (the
// conformance-contract name; kebab-cased by the Go SDK). Delegates to workflows.
func (m *Dagmar) DagmarBootstrap(
	ctx context.Context,
	// source is the Project's source tree (CI contract: `dagger call -m .dagmar dagmar-bootstrap --source .`).
	source *dagger.Directory,
) (string, error) {
	return workflows.Bootstrap(ctx, source)
}

// DagmarGate is dagmar's gate-family VERIFY step (ADR-0009 §2 / ADR-0012 §4): an always-Dagger
// function that runs the manifest-declared checkables (`.dagmar/project.yaml`) — manifest = what,
// dagmar-gate = how (review-11 GAP-3). Reused in CI (`dagger call -m .dagmar dagmar-gate --source .`)
// AND in-loop (coder self-verification, Phase 2). Exposes as the dagger function `dagmar-gate`.
// Delegates to workflows.
func (m *Dagmar) DagmarGate(
	ctx context.Context,
	// source is the Project's source tree (CI contract: `dagger call -m .dagmar dagmar-gate --source .`).
	source *dagger.Directory,
) (string, error) {
	return workflows.Gate(ctx, source)
}
