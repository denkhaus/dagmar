// Package workflows contains dagmar-as-a-Project's hook implementations.
//
// dagmar-issues and dagmar-memory (ADR-0019 D2) are LLM-Tool hooks: Dagger module functions
// registered via Env.WithMainModule() that the LLM calls as native tools during the loop.
// They operate on the project worktree (.seeds/, .mulch/) implicitly — no source parameter.
//
// Vendor-agnosticism (ADR-0017 §7): the function name is the contract. The implementation
// delegates to the project's backing service (seeds/mulch). A different project would
// implement these functions against a different backend (Linear, GitHub Issues, etc.).
// The backing CLI (sd/ml) is installed inside a container per-call — the module function
// builds a container with the CLI, mounts the workspace, and runs the command.
package workflows

import (
	"context"
	"fmt"

	"dagger/dagmar-project/internal/dagger"
)

// cliImage is the base image for CLI-based hook execution. It carries the tools needed to
// install sd/ml (curl, sh). A heavier base (golang) would be needed if the CLIs are Go-built
// from source, but the published binaries are self-contained.
const cliImage = "debian:12-slim"

// sdVersion/mlVersion pin the CLI versions used by the hooks. Pinned for reproducibility.
const (
	sdVersion = "0.5.15"
	mlVersion = "0.10.7"
)

// DagmarIssues implements the dagmar-issues LLM-Tool hook (ADR-0019 D2).
//
// It delegates to the project's issue tracker CLI (seeds: `sd`). The function name is the
// contract (ADR-0017 §7). A project using a different tracker implements this function
// differently; dagmar calls it the same way.
//
// The hook runs `sd` inside a container with the workspace mounted — the CLI is not required
// on the host or in the LLM's sandbox. This is the vendor-agnostic pattern: the backing
// service is an implementation detail of this function, invisible to the LLM caller.
func DagmarIssues(
	ctx context.Context,
	source *dagger.Directory,
	action string,
	id string,
	query string,
	title string,
	body string,
) (string, error) {
	ctr, err := sdContainer(source)
	if err != nil {
		return "", err
	}

	switch action {
	case "read":
		if id == "" {
			return "", fmt.Errorf("dagmar-issues: read requires an issue id")
		}
		return ctr.WithExec([]string{"sd", "show", id, "--format", "markdown"}).
			Stdout(ctx)

	case "search":
		if query == "" {
			return "", fmt.Errorf("dagmar-issues: search requires a query")
		}
		return ctr.WithExec([]string{"sd", "search", query, "--format", "compact"}).
			Stdout(ctx)

	case "create":
		if title == "" {
			return "", fmt.Errorf("dagmar-issues: create requires a title")
		}
		args := []string{"sd", "create", "--title", title, "--type", "task", "--priority", "2"}
		if body != "" {
			args = append(args, "--description", body)
		}
		return ctr.WithExec(args).Stdout(ctx)

	case "update":
		if id == "" {
			return "", fmt.Errorf("dagmar-issues: update requires an issue id")
		}
		args := []string{"sd", "update", id}
		if body != "" {
			args = append(args, "--description", body)
		}
		return ctr.WithExec(args).Stdout(ctx)

	default:
		return "", fmt.Errorf("dagmar-issues: unknown action %q (want read|search|create|update)", action)
	}
}

// DagmarMemory implements the dagmar-memory LLM-Tool hook (ADR-0019 D2).
//
// It delegates to the project's expertise store (mulch: `ml`). Same vendor-agnostic pattern
// as DagmarIssues.
func DagmarMemory(
	ctx context.Context,
	source *dagger.Directory,
	action string,
	query string,
	key string,
	value string,
) (string, error) {
	ctr, err := mlContainer(source)
	if err != nil {
		return "", err
	}

	switch action {
	case "read":
		if query == "" {
			return ctr.WithExec([]string{"ml", "prime", "--manifest"}).Stdout(ctx)
		}
		return ctr.WithExec([]string{"ml", "prime", query}).Stdout(ctx)

	case "search":
		if query == "" {
			return "", fmt.Errorf("dagmar-memory: search requires a query")
		}
		return ctr.WithExec([]string{"ml", "search", query}).Stdout(ctx)

	case "write":
		if key == "" || value == "" {
			return "", fmt.Errorf("dagmar-memory: write requires both key (domain) and value (description)")
		}
		return ctr.WithExec([]string{"ml", "record", key, "--type", "pattern", "--description", value}).
			Stdout(ctx)

	default:
		return "", fmt.Errorf("dagmar-memory: unknown action %q (want read|search|write)", action)
	}
}

// sdContainer builds a container with `sd` installed and the workspace mounted at /workspace.
// The seeds CLI is downloaded as a self-contained binary (Go-compiled, no runtime deps).
func sdContainer(source *dagger.Directory) (*dagger.Container, error) {
	if source == nil {
		return nil, fmt.Errorf("dagmar-issues: source directory is required")
	}
	return dagger.Connect().Container().From(cliImage).
		WithExec([]string{"sh", "-c",
			fmt.Sprintf("apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && "+
				"curl -fsSL https://github.com/jayminwest/seeds/releases/download/v%s/seeds-linux-amd64 -o /usr/local/bin/sd && "+
				"chmod +x /usr/local/bin/sd", sdVersion)}).
		WithMountedDirectory("/workspace", source).
		WithWorkdir("/workspace"), nil
}

// mlContainer builds a container with `ml` installed and the workspace mounted at /workspace.
// The mulch CLI is downloaded as a self-contained binary.
func mlContainer(source *dagger.Directory) (*dagger.Container, error) {
	if source == nil {
		return nil, fmt.Errorf("dagmar-memory: source directory is required")
	}
	return dagger.Connect().Container().From(cliImage).
		WithExec([]string{"sh", "-c",
			fmt.Sprintf("apt-get update && apt-get install -y --no-install-recommends curl ca-certificates && "+
				"curl -fsSL https://github.com/jayminwest/mulch/releases/download/v%s/mulch-linux-amd64 -o /usr/local/bin/ml && "+
				"chmod +x /usr/local/bin/ml", mlVersion)}).
		WithMountedDirectory("/workspace", source).
		WithWorkdir("/workspace"), nil
}
