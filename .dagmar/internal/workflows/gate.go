// gate.go — dagmar-gate, the always-Dagger verify wrapper (ADR-0009 §2 / ADR-0012 §4).
package workflows

import (
	"context"
	"fmt"
	"path"
	"strings"

	"dagger/dagmar-project/internal/dagger"
	"github.com/denkhaus/dagmar/manifest"
)

// Gate is dagmar-gate: the always-Dagger verify wrapper that runs the manifest-declared
// checkables (ADR-0003 = what, gate = how — review-11 GAP-3). It reads `.dagmar/project.yaml`
// from the source, runs each checkable in the mise-bootstrapped container (bootstrapBase —
// debian + the mise.toml toolchain), and returns a summary; a non-zero checkable aborts the
// gate with an error (so CI fails). Pure VERIFY — it does not roll out the toolchain itself
// (dagmar-bootstrap does; the gate reuses that base as a Dagger-cached layer). Networked container
// — the gate is the deterministic-networked layer (ADR-0011); hermeticity is the LLM-loop
// constraint, not the gate's.
//
// dagmar-gate is reused in CI (GitHub Actions: `dagger call -m .dagmar dagmar-gate --source .`)
// AND in-loop (coder self-verification, Phase 2). The manifest parser is the PUBLISHED platform
// contract (github.com/denkhaus/dagmar/manifest, ADR-0014 GAP-1 resolved by dagmar-a1e0) — both
// the platform (.dagger) and this project module depend on it by versioned require.
func Gate(ctx context.Context, source *dagger.Directory) (string, error) {
	raw, err := source.File(".dagmar/project.yaml").Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("dagmar-gate: read .dagmar/project.yaml: %w", err)
	}
	pm, err := manifest.ParseManifest([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("dagmar-gate: %w", err)
	}

	var summaries []string
	for _, c := range pm.Checkables {
		out, exit, err := runCheckable(ctx, source, c)
		if err != nil {
			return "", fmt.Errorf("dagmar-gate: checkable %q: %w", c.Name, err)
		}
		if exit != 0 {
			// Include the failing output so CI / the coder sees why.
			return "", fmt.Errorf("dagmar-gate: checkable %q FAILED (exit %d)\n--- output ---\n%s",
				c.Name, exit, strings.TrimSpace(out))
		}
		summaries = append(summaries, fmt.Sprintf("  ✓ %s", c.Name))
	}
	return fmt.Sprintf("dagmar-gate: all %d checkable(s) passed\n%s",
		len(pm.Checkables), strings.Join(summaries, "\n")), nil
}

// runCheckable runs one checkable in a golang container and returns (stdout, exitCode, err).
// The exit code is captured explicitly (DAGMAR_EXIT) with Expect=Any so a non-zero exit yields
// the output rather than an opaque exec error.
func runCheckable(ctx context.Context, source *dagger.Directory, c manifest.Checkable) (string, int, error) {
	// Derive from the mise-bootstrapped base (bootstrapBase: tools on PATH via mise shims). The
	// bootstrap layer is a Dagger cache hit once realized by dagmar-bootstrap or a prior checkable.
	// Override workdir to the checkable's (bootstrapBase mounts /src + sets workdir /src).
	ctr := bootstrapBase(source).
		WithWorkdir(path.Join("/src", c.Workdir))
	for k, v := range c.Env {
		ctr = ctr.WithEnvVariable(k, v)
	}
	// Merge stderr into stdout for the WHOLE command chain (Go toolchain diagnostics —
	// build/vet/test errors — go to stderr; review-14 FIX-1). `exec 2>&1` redirects the shell's
	// fd 2→1 for the rest of the script (a bare trailing `2>&1` would bind only to the last `&&`
	// command). Then append the exit-code marker. ReturnTypeAny keeps the exec from erroring on
	// non-zero, so a failing checkable's "why" reaches the abort message.
	cmd := `exec 2>&1; ` + c.Command + `; echo "DAGMAR_EXIT=$?"`
	out, err := ctr.WithExec([]string{"sh", "-c", cmd},
		dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).Stdout(ctx)
	if err != nil {
		return "", 0, err
	}
	exit := parseDagmarExit(out)
	return out, exit, nil
}

// parseDagmarExit extracts the exit code from the LAST "DAGMAR_EXIT=<n>" marker in the output
// (review-14 FIX-2 — the LAST one is the real marker after the command chain; an earlier literal
// line in the command's own output must not mask a real failure as pass). Defaults to 1 (fail) if
// no marker is present (the command did not reach the echo — treat as failure).
func parseDagmarExit(out string) int {
	last := -1
	for _, line := range strings.Split(out, "\n") {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "DAGMAR_EXIT=%d", &n); err == nil {
			last = n
		}
	}
	if last < 0 {
		return 1
	}
	return last
}
