// bootstrap.go — dagmar-bootstrap, the always-Dagger prepare wrapper (ADR-0009 §2 / ADR-0012 §4).
package workflows

import (
	"context"
	"fmt"
	"path"
	"strings"

	"dagger/dagmar/internal/config"
	"dagger/dagmar/internal/dagger"
)

// Bootstrap is dagmar-bootstrap: the prepare wrapper run once per workspace before verification
// (HOUSE-5). For a Go project it resolves + warms module dependencies (`go mod download`) in each
// of the manifest checkables' workdirs — proving deps resolve and populating the build cache. It
// does not itself verify (that is dagmar-gate); it prepares.
//
// CI note (review-14 HOUSE-1): for dagmar-own the gate is self-sufficient (Go's `go build`
// resolves deps inline), so .github/workflows/ci.yml invokes only dagmar-gate. dagmar-bootstrap
// exists as the lifecycle PREPARE step and is worth running before the gate for Projects whose
// prepare is expensive (codegen, protoc, heavy dep trees) — it is available, just not required
// for Go's gate.
func Bootstrap(ctx context.Context, source *dagger.Directory) (string, error) {
	raw, err := source.File(".dagmar/project.yaml").Contents(ctx)
	if err != nil {
		return "", fmt.Errorf("dagmar-bootstrap: read .dagmar/project.yaml: %w", err)
	}
	manifest, err := config.ParseManifest([]byte(raw))
	if err != nil {
		return "", fmt.Errorf("dagmar-bootstrap: %w", err)
	}

	// Dedup workdirs (a module may have several checkables sharing one workdir).
	seen := map[string]bool{}
	var summaries []string
	for _, c := range manifest.Checkables {
		if seen[c.Workdir] {
			continue
		}
		seen[c.Workdir] = true
		ctr := dagger.Connect().Container().
			From(gateImage).
			WithMountedDirectory("/src", source).
			WithWorkdir(path.Join("/src", c.Workdir))
		out, err := ctr.WithExec([]string{"go", "mod", "download"}).Stdout(ctx)
		if err != nil {
			return "", fmt.Errorf("dagmar-bootstrap: go mod download in %q: %w\n%s",
				c.Workdir, err, strings.TrimSpace(out))
		}
		summaries = append(summaries, fmt.Sprintf("  ✓ deps resolved: %s", c.Workdir))
	}
	return fmt.Sprintf("dagmar-bootstrap: prepared %d workdir(s)\n%s",
		len(seen), strings.Join(summaries, "\n")), nil
}
