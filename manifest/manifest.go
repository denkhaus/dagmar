// manifest.go — ProjectManifest types + parsing (ADR-0003). PURE (no Dagger import); see doc.go
// for the package's authority + load-resolution story.

package manifest

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// Version is the manifest contract version this library implements (CRD-analogous: the platform
// pins which schema revision it validates/speaks). SemVer; bump on incompatible manifest changes.
// The platform module references this to declare the contract version it conforms to.
const Version = "v0.1.0"

// ProjectManifest is the in-repo conformance contract each Project exposes at
// `.dagmar/project.yaml` (ADR-0003): project-specific metadata (+ Project Hook binding +
// repo/flow config, added as the control plane grows). Phase 1 carries only the checkables.
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
