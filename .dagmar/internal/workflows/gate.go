// gate.go — dagmar-gate, the always-Dagger verify wrapper (ADR-0009 §2 / ADR-0012 §4).
package workflows

import (
	"context"
	"fmt"
	"path"
	"strconv"
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
//
// coverageFloorBps is the ratcheted coverage floor in basis points (0–10000, e.g. 7850 = 78.50%).
// When > 0, the gate additionally runs `go test -coverprofile` in the root module and compares
// total coverage against the floor (dagmar-4154). 0 = coverage check disabled.
func Gate(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret, coverageFloorBps int) (string, error) {
	raw, err := source.File(".dagmar/project.yaml").Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("dagmar-gate: read .dagmar/project.yaml: %w", err)
	}
	pm, err := manifest.ParseManifest([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("dagmar-gate: %w", err)
	}

	var summaries []string
	totalChecks := len(pm.Checkables)
	for _, c := range pm.Checkables {
		out, exit, err := runCheckable(ctx, source, c, githubToken)
		if err != nil {
			return "", fmt.Errorf("dagmar-gate: checkable %q: %w", c.Name, err)
		}
		if exit != 0 {
			// Include the failing output so CI / the coder sees why.
			return "", fmt.Errorf("dagmar-gate: checkable %q FAILED (exit %d)\n--- output ---\n%s",
				c.Name, exit, strings.TrimSpace(out))
		}
		summaries = append(summaries, fmt.Sprintf("  \u2713 %s", c.Name))
	}

	// Coverage check (dagmar-4154): when a floor is set, measure total go test coverage and
	// compare it against the ratcheted floor. The floor is expressed in basis points (0–10000)
	// to avoid float64 in CRD schemas.
	if coverageFloorBps > 0 {
		coverageBps, err := runCoverageCheck(ctx, source, githubToken, coverageFloorBps)
		if err != nil {
			return "", err
		}
		summaries = append(summaries,
			fmt.Sprintf("  \u2713 coverage: %s (floor: %s)",
				formatBps(coverageBps), formatBps(coverageFloorBps)))
		totalChecks++
	}

	return fmt.Sprintf("dagmar-gate: all %d checkable(s) passed\n%s",
		totalChecks, strings.Join(summaries, "\n")), nil
}

// runCoverageCheck runs `go test -coverprofile=coverage.out ./...` in the root module (reusing
// the same mise-bootstrapped container), parses total coverage from `go tool cover -func`, and
// returns it in basis points. If measured coverage is below the floor, the gate goes RED.
func runCoverageCheck(ctx context.Context, source *dagger.Directory, githubToken *dagger.Secret, floorBps int) (int, error) {
	ctr := bootstrapBase(source, githubToken).WithWorkdir("/src")

	// Run tests with coverage in the root module. Merge stderr→stdout and capture exit code
	// (same DAGMAR_EXIT pattern as runCheckable). A non-zero exit means the tests themselves
	// failed — the coverage gate cannot proceed.
	cmd := `exec 2>&1; go test -coverprofile=coverage.out ./...; echo "DAGMAR_EXIT=$?"`
	out, err := ctr.WithExec([]string{"sh", "-c", cmd},
		dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny}).Stdout(ctx)
	if err != nil {
		return 0, fmt.Errorf("dagmar-gate: coverage: go test: %w", err)
	}
	if exit := parseDagmarExit(out); exit != 0 {
		return 0, fmt.Errorf("dagmar-gate: coverage: go test FAILED (exit %d)\n--- output ---\n%s",
			exit, strings.TrimSpace(out))
	}

	// Parse the total coverage from `go tool cover -func=coverage.out`. The last line is:
	//   total:		(statements)	82.3%
	coverOut, err := ctr.WithExec([]string{"sh", "-c", "go tool cover -func=coverage.out"}).Stdout(ctx)
	if err != nil {
		return 0, fmt.Errorf("dagmar-gate: coverage: go tool cover: %w", err)
	}
	coverageBps, ok := parseCoverTotal(coverOut)
	if !ok {
		return 0, fmt.Errorf("dagmar-gate: coverage: could not parse total from `go tool cover`:\n%s",
			strings.TrimSpace(coverOut))
	}

	if coverageBps < floorBps {
		return 0, fmt.Errorf("dagmar-gate: coverage BELOW floor: %s < %s",
			formatBps(coverageBps), formatBps(floorBps))
	}
	return coverageBps, nil
}

// parseCoverTotal extracts the total coverage percentage from `go tool cover -func` output and
// returns it as basis points (int, 0–10000). The total line has the form:
//
//	total:		(statements)	82.3%
//
// Returns (bps, true) on success, (0, false) if no total line was found.
func parseCoverTotal(coverOut string) (int, bool) {
	lines := strings.Split(coverOut, "\n")
	// The total line is always the last non-empty line starting with "total:".
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		// Extract the trailing percentage: everything after the last tab.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pctStr := strings.TrimSuffix(fields[len(fields)-1], "%")
		pct, err := strconv.ParseFloat(pctStr, 64)
		if err != nil {
			continue
		}
		bps := int(pct*100 + 0.5) // round to nearest basis point
		return bps, true
	}
	return 0, false
}

// formatBps converts basis points (7850) to a human-readable percentage string ("78.50%").
func formatBps(bps int) string {
	return fmt.Sprintf("%.2f%%", float64(bps)/100)
}

// runCheckable runs one checkable in a golang container and returns (stdout, exitCode, err).
// The exit code is captured explicitly (DAGMAR_EXIT) with Expect=Any so a non-zero exit yields
// the output rather than an opaque exec error.
func runCheckable(ctx context.Context, source *dagger.Directory, c manifest.Checkable, githubToken *dagger.Secret) (string, int, error) {
	// Derive from the mise-bootstrapped base (bootstrapBase: tools on PATH via mise shims). The
	// bootstrap layer is a Dagger cache hit once realized by dagmar-bootstrap or a prior checkable.
	// Override workdir to the checkable's (bootstrapBase mounts /src + sets workdir /src).
	ctr := bootstrapBase(source, githubToken).
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
