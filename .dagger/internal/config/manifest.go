// ProjectManifest types + parsing for the in-repo conformance contract (ADR-0003).
//
// This file is PURE: it defines the manifest schema and parses YAML, with no Dagger import, so it
// is unit-testable without an engine. Reading the manifest bytes out of a *dagger.Directory is a
// Dagger I/O concern and lives in the workflows/ layer (workflows.Gate); this package only turns
// raw bytes into a typed manifest.
package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ProjectManifest is the in-repo conformance contract each Project exposes at
// `.dagmar/project.yaml` (ADR-0003): project-specific `checkables` (+ os-eco binding +
// repo/flow metadata, added as the control plane grows). Phase 1 carries only the checkables.
type ProjectManifest struct {
	// Checkables are the project's mechanical self-verification steps (build/test/lint), declared
	// per-project and required (a Project without them is non-conforming). dagmar-gate (ADR-0009 §2
	// / ADR-0012 §4) is the always-Dagger wrapper that RUNS them — manifest = what, gate = how.
	Checkables []Checkable `yaml:"checkables"`
}

// Checkable is one named verification step the gate runs in a container.
type Checkable struct {
	// Name labels the step (for reporting).
	Name string `yaml:"name"`
	// Workdir is the path within the source where the command runs (e.g. "." or ".dagger").
	Workdir string `yaml:"workdir"`
	// Command is the shell command the gate runs (exit 0 = pass).
	Command string `yaml:"command"`
	// Env is optional extra environment for the command (e.g. {"GOWORK": "off"}).
	Env map[string]string `yaml:"env,omitempty"`
}

// ParseManifest unmarshals a ProjectManifest from raw YAML bytes. Pure.
func ParseManifest(raw []byte) (*ProjectManifest, error) {
	var m ProjectManifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse .dagmar/project.yaml: %w", err)
	}
	if len(m.Checkables) == 0 {
		return nil, fmt.Errorf("manifest has no checkables: a Project must declare at least one (ADR-0003)")
	}
	for i, c := range m.Checkables {
		if c.Name == "" || c.Command == "" {
			return nil, fmt.Errorf("checkable #%d must have both name and command", i)
		}
	}
	return &m, nil
}
