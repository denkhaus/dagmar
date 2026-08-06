// bootstrap.go — dagmar-bootstrap, the always-Dagger prepare wrapper (ADR-0009 §2 / ADR-0012 §4).
package workflows

import (
	"context"
	"fmt"

	"dagger/dagmar-project/internal/dagger"
)

// bootstrapBaseImage is the minimal base the gate's toolchain is rolled out onto. A glibc base so
// mise's release binary runs (musl bases like alpine would need a musl mise build). debian:12-slim
// (docker.io, reachable); chainguard was preferred but cgr.io is DNS-unresolvable on the dev
// network — revisit if that changes. mise provides EVERY tool declared in mise.toml (go,
// betterleaks, …); mise.toml is the SINGLE tool source for local dev, CI-direct, and the gate.
const bootstrapBaseImage = "debian:12-slim"

// bootstrapBase returns the gate's prepared container: debian base + mise + the project's
// mise.toml toolchain rolled out + shims on PATH + source mounted at /src. It is lazy (the
// mise-install exec is not realized until a caller executes it). dagmar-bootstrap realizes it
// (standalone PREPARE); dagmar-gate derives each checkable from it (pure verify).
//
// Dagger caches the mise-install exec layer content-addressed (by mise.toml + base), so the
// rollout is realized ONCE and reused as a cache hit by dagmar-bootstrap AND every gate checkable
// — no redundant mise-install when the gate runs in-loop against an already-prepared workspace.
//
// NOTE: `mise install` rolls out ALL mise.toml tools (single source). A few are dev/runner-only
// (lefthook, nushell, golangci-lint) and unused inside a checkable; installing them is the cost of
// one config, amortized by the layer cache. Narrow to a subset only if CI overhead bites.
func bootstrapBase(source *dagger.Directory) *dagger.Container {
	return dagger.Connect().Container().
		From(bootstrapBaseImage).
		// Prereqs for the mise installer (curl + TLS roots).
		WithExec([]string{"sh", "-c", "apt-get update && apt-get install -y --no-install-recommends curl ca-certificates"}).
		// Install mise (single static binary via the official installer).
		WithExec([]string{"sh", "-c", "curl -fsSL https://mise.run | sh"}).
		// Project source (mise.toml lives here) + workdir so trust/install resolve it.
		WithMountedDirectory("/src", source).
		WithWorkdir("/src").
		// Trust the project config non-interactively, then roll out the toolchain.
		WithExec([]string{"sh", "-c", "/root/.local/bin/mise trust && /root/.local/bin/mise install"}).
		// mise shims (+ the mise binary) on PATH so checkables resolve go/betterleaks directly.
		WithEnvVariable("PATH", "/root/.local/share/mise/shims:/root/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
}

// Bootstrap is dagmar-bootstrap: the prepare wrapper that rolls out the project's mise toolchain
// (mise.toml) into the gate container. Run once per workspace before verification — by CI, lefthook
// pre-push, or the agent setup. dagmar-gate derives its checkables from the same bootstrapped
// base, so the rollout is realized once (Dagger-cached) and reused.
func Bootstrap(ctx context.Context, source *dagger.Directory) (string, error) {
	// Run `mise ls` to force realization of the mise-install layer (otherwise the chain is lazy)
	// and report the rolled-out toolchain. A failure here means the rollout itself failed.
	out, err := bootstrapBase(source).
		WithExec([]string{"sh", "-c", "/root/.local/bin/mise ls"}).
		Stdout(ctx)
	if err != nil {
		return "", fmt.Errorf("dagmar-bootstrap: mise toolchain rollout failed: %w\n%s", err, out)
	}
	return fmt.Sprintf("dagmar-bootstrap: mise toolchain rolled out (mise.toml)\n%s", out), nil
}
