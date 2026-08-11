// Package workflows contains dagmar-as-a-Project's hook implementations.
//
// dagmar-issues and dagmar-memory (ADR-0019 D2) are LLM-Tool hooks: Dagger module functions
// registered via Env.WithMainModule() that the LLM calls as native tools during the loop.
// They operate on the project worktree (.seeds/, .mulch/) implicitly — no source parameter.
// The workspace source is accessible inside the module via dag.CurrentModule().Source().
package workflows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DagmarIssues implements the dagmar-issues LLM-Tool hook (ADR-0019 D2).
//
// It delegates to the project's issue tracker CLI (seeds: `sd`). The function name is the
// contract (ADR-0017 §7: backing-service names appear only inside hook implementations, never
// in dagmar's domain code). A project using a different tracker (Linear, GitHub Issues)
// implements this function differently; dagmar calls it the same way.
//
// Actions:
//   - "read":   read a single issue by id
//   - "search": full-text search across issues
//   - "create": create a new issue
//   - "update": update an existing issue
//
// Output is formatted for LLM consumption (plain text). A project without issue tracking
// implements this as a noop (returns empty/"noop").
func DagmarIssues(
	ctx context.Context,
	action string,
	id string,
	query string,
	title string,
	body string,
) (string, error) {
	switch action {
	case "read":
		if id == "" {
			return "", fmt.Errorf("dagmar-issues: read requires an issue id")
		}
		out, err := runInWorkdir(ctx, "sd", "show", id, "--format", "markdown")
		if err != nil {
			return "", fmt.Errorf("dagmar-issues: read %s: %w", id, err)
		}
		return out, nil

	case "search":
		if query == "" {
			return "", fmt.Errorf("dagmar-issues: search requires a query")
		}
		out, err := runInWorkdir(ctx, "sd", "search", query, "--format", "compact")
		if err != nil {
			return "", fmt.Errorf("dagmar-issues: search %q: %w", query, err)
		}
		return out, nil

	case "create":
		if title == "" {
			return "", fmt.Errorf("dagmar-issues: create requires a title")
		}
		args := []string{"create", "--title", title, "--type", "task", "--priority", "2"}
		if body != "" {
			args = append(args, "--description", body)
		}
		out, err := runInWorkdir(ctx, "sd", args...)
		if err != nil {
			return "", fmt.Errorf("dagmar-issues: create: %w", err)
		}
		return fmt.Sprintf("Created issue: %s", strings.TrimSpace(out)), nil

	case "update":
		if id == "" {
			return "", fmt.Errorf("dagmar-issues: update requires an issue id")
		}
		// Update is intentionally limited — the LLM should not close/reopen issues
		// without controller oversight. This hook supports status annotation only.
		args := []string{"update", id}
		if body != "" {
			args = append(args, "--description", body)
		}
		out, err := runInWorkdir(ctx, "sd", args...)
		if err != nil {
			return "", fmt.Errorf("dagmar-issues: update %s: %w", id, err)
		}
		return fmt.Sprintf("Updated issue %s: %s", id, strings.TrimSpace(out)), nil

	default:
		return "", fmt.Errorf("dagmar-issues: unknown action %q (want read|search|create|update)", action)
	}
}

// DagmarMemory implements the dagmar-memory LLM-Tool hook (ADR-0019 D2).
//
// It delegates to the project's expertise store (mulch: `ml`). The function name is the
// contract (ADR-0017 §7). A project without memory implements this as a noop.
//
// Actions:
//   - "read":   load all expertise for a domain
//   - "search": full-text search across expertise records
//   - "write":  record a new expertise entry
func DagmarMemory(
	ctx context.Context,
	action string,
	query string,
	key string,
	value string,
) (string, error) {
	switch action {
	case "read":
		if query == "" {
			// Read all domains if no specific domain requested
			out, err := runInWorkdir(ctx, "ml", "prime", "--manifest")
			if err != nil {
				return "", fmt.Errorf("dagmar-memory: read: %w", err)
			}
			return out, nil
		}
		out, err := runInWorkdir(ctx, "ml", "prime", query)
		if err != nil {
			return "", fmt.Errorf("dagmar-memory: read %s: %w", query, err)
		}
		return out, nil

	case "search":
		if query == "" {
			return "", fmt.Errorf("dagmar-memory: search requires a query")
		}
		out, err := runInWorkdir(ctx, "ml", "search", query)
		if err != nil {
			return "", fmt.Errorf("dagmar-memory: search %q: %w", query, err)
		}
		return out, nil

	case "write":
		if key == "" || value == "" {
			return "", fmt.Errorf("dagmar-memory: write requires both key (domain) and value (description)")
		}
		out, err := runInWorkdir(ctx, "ml", "record", key, "--type", "pattern", "--description", value)
		if err != nil {
			return "", fmt.Errorf("dagmar-memory: write: %w", err)
		}
		return fmt.Sprintf("Recorded expertise: %s", strings.TrimSpace(out)), nil

	default:
		return "", fmt.Errorf("dagmar-memory: unknown action %q (want read|search|write)", action)
	}
}

// runInWorkdir runs a command in the current module's work directory. The LLM-Tool hooks
// operate on the project worktree implicitly (ADR-0019 D2) — the workspace source is
// accessible via dag.CurrentModule().Source(), but the CLIs (sd, ml) read their stores
// from the current working directory. When running inside a Dagger module, the workdir
// is the module's source directory.
func runInWorkdir(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workdir()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w (output: %s)", name, strings.Join(args, " "), err, string(out))
	}
	return strings.TrimSpace(string(out)), nil
}

// workdir returns the working directory for CLI invocations. Prefers the DAGGER_MODULE_SOURCE
// env (set by the Dagger engine when running module functions), falls back to cwd.
func workdir() string {
	if dir := os.Getenv("DAGGER_MODULE_SOURCE"); dir != "" {
		return dir
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	return dir
}
