// manifest.go — ProjectManifest types + parsing (ADR-0003). PURE (no Dagger import) → unit-
// testable without an engine; reading the manifest bytes out of a *dagger.Directory is a Dagger
// I/O concern in workflows.Gate.
//
// ADR-0014 scope note: the manifest SCHEMA is platform authority — projects author instances,
// not the types. It lives in the PROJECT module anyway because Dagger loads each module in
// source isolation: a project→platform Go dep (relative `replace` to a sibling dir) cannot
// resolve at module-load time (the sibling is absent from the load context). So the canonical
// schema travels with the gate that consumes it. Authority is enforced by convention + the
// future conformance probe (ADR-0014 Q4); when a second Project needs the parser, extract it
// into a PUBLISHED shared library both modules depend on by version (not relative replace).
package config

import (
	"fmt"
	"path"
	"strings"

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
		if err := validateWorkdir(c.Workdir); err != nil {
			return nil, fmt.Errorf("checkable %q workdir: %w", c.Name, err)
		}
	}
	return &m, nil
}

// validateWorkdir rejects workdirs that are empty, absolute, or escape the source root via ".."
// (review-14 HOUSE-2). The gate mounts the source at /src and runs in path.Join("/src", workdir);
// a workdir like ".." or "/etc" must not be allowed to escape the mounted source.
func validateWorkdir(workdir string) error {
	if workdir == "" {
		return fmt.Errorf("empty (use \".\" for the source root)")
	}
	if path.IsAbs(workdir) {
		return fmt.Errorf("absolute paths not allowed (must be relative to the source root)")
	}
	for _, elem := range strings.Split(path.Clean(workdir), "/") {
		if elem == ".." {
			return fmt.Errorf(`".." components not allowed (must stay within the source root)`)
		}
	}
	return nil
}
