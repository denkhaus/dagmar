// Package app contains dagmar's application services: orchestration that wires the Dagger
// SDK (Tier A, used DIRECTLY — ADR-0001; ADR-0010 §3) to the pure domain core. app is the
// boundary the main Dagger object (Dagmar) delegates to. Dagger-direct logic here is
// integration-tested via a real engine (//go:build integration).
package app

import (
	"dagger/dagmar/internal/dagger"
	"dagger/dagmar/internal/domain"
)

// BuildSandbox realizes a domain.SandboxSpec as a Dagger Container (Tier A, direct). The
// spec is validated HERE, at the top of the binding (the functional-core contract,
// ADR-0010 §3), so that NO caller — now or future — can skip it: a malformed spec surfaces
// as a clean domain error (e.g. "domain: SandboxSpec.Image is required") instead of an
// opaque engine failure like Container().From(""). No LLM, no network beyond the base-image
// pull — the cheap v0 vertical proving the layout seams.
func BuildSandbox(spec domain.SandboxSpec) (*dagger.Container, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	// dagger.Connect() returns the SDK's process-wide singleton client (set in
	// internal/dagger's init() when the Dagger runtime provides DAGGER_SESSION_PORT).
	// The main package uses the package-local `dag`; sub-packages use Connect() — same
	// client. Tier A is used directly here (no port); this path is integration-tested.
	ctr := dagger.Connect().Container().From(spec.Image)
	if spec.Workdir != "" {
		ctr = ctr.WithWorkdir(spec.Workdir)
	}
	return ctr, nil
}
