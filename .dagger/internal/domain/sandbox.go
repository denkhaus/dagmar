// Package domain holds dagmar's pure functional core: value specifications and decision
// functions with ZERO dependencies on Dagger or any external SDK (ADR-0010 §3). This is
// the maximally testable layer — table-tested without a Dagger engine.
package domain

import "errors"

// SandboxSpec is the pure specification of a Sandbox. A Sandbox is an isolated,
// credentialed execution slot subordinate to the Engine (1 Run : 1 Sandbox; CONTEXT.md).
// The spec carries resource/tooling intent only; the Dagger binding (app.BuildSandbox)
// realizes it as a *dagger.Container. Pure — no Dagger import.
type SandboxSpec struct {
	// Image is the base OCI image for the Sandbox container.
	Image string
	// Workdir is the working directory inside the Sandbox (empty = image default).
	Workdir string
}

// Validate checks the spec is well-formed. Pure, no side effects — this is the kind of
// decision logic that earns a unit test in domain/.
func (s SandboxSpec) Validate() error {
	if s.Image == "" {
		return errors.New("domain: SandboxSpec.Image is required")
	}
	return nil
}
