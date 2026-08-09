// Package prompt implements dagmar's cross-store prompt composition (ADR-0005, Variant A).
//
// STATUS (Review 29 A2/A3): The full canopy-based composition (render project prompt +
// dagmar mixins via `cn`, merge in Go via Compose()) is STRUCTURALLY PRESENT but NOT
// FUNCTIONAL at runtime: the agent pod does not install canopy CLI (`cn`), so every
// `cn render` call fails. The controller currently uses ShellComposeCommand which falls
// back to a minimal stub prompt. This is acceptable for the cognition proof (dagmar-9571);
// the runtime delivery (provisioning `cn` in the pod, rendering DagmarMixins, calling
// Compose()) is deferred to a follow-up seed (ADR-0005 runtime delivery).
//
// dagmar-prompt (ADR-0019) is a separate in-loop LLM tool for on-demand project prompt
// rendering — it supplements this composition, not replaces it.
package prompt

import (
	"fmt"
	"strings"
)

// Compose builds the final prompt markdown from project sections and dagmar mixin sections.
// This is the Go-side merge (ADR-0005 Variant A step 3): canopy's resolution rules are
// applied here — parent → mixins → self; same-name section = last wins.
//
// The project sections come from `cn render <projectPrompt> --format json` in the project's
// .canopy/ store. The dagmar sections come from `cn render <mixin> --format json` in dagmar's
// own .canopy/ store. Both are rendered as section sets before this function merges them.
//
// STATUS: unit-tested but NOT wired into the runtime dispatch path. The controller uses
// ShellComposeCommand (which renders at most a stub). Full wiring deferred (see package doc).
func Compose(projectSections, dagmarSections []Section) string {
	merged := make(map[string]string)

	// Resolution order: dagmar mixins first (they set operational defaults like output
	// format, safety bounds, tool rules), then project content (overrides/extends).
	// Same-name section = last wins (ADR-0005).
	for _, s := range dagmarSections {
		merged[s.Name] = s.Body
	}
	for _, s := range projectSections {
		merged[s.Name] = s.Body
	}

	// Emit as ## section headings (canopy's native output format).
	var b strings.Builder
	for _, name := range orderedSectionNames(merged) {
		b.WriteString("## ")
		b.WriteString(name)
		b.WriteString("\n\n")
		b.WriteString(merged[name])
		b.WriteString("\n\n")
	}
	return b.String()
}

// Section is one named section of a canopy prompt (name + body).
type Section struct {
	Name string
	Body string
}

// orderedSectionNames returns section names in a stable order (alphabetical for now;
// canopy's resolve order is already applied via the map's last-wins semantics).
func orderedSectionNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// ShellRenderCommand builds the shell command to render a canopy prompt to a .md file
// inside a container. `cn render <name> --format text` renders the resolved prompt to stdout.
// STATUS: NOT called by any production code path (Review 29 A3). Retained for the future
// runtime delivery when canopy CLI is provisioned in the agent pod.
func ShellRenderCommand(promptName string, canopyDir string, outputPath string) string {
	return fmt.Sprintf(
		`cd %s && cn render %s --format text > %s`,
		canopyDir, promptName, outputPath,
	)
}

// ShellComposeCommand builds a shell command that writes the agent prompt to outputPath.
//
// CURRENT BEHAVIOR (Review 29 A2 fix): canopy CLI (`cn`) is NOT installed in the agent pod
// yet. The command attempts `cn render` first; when it fails (command not found), the
// fallback writes a functional stub prompt with real newlines (printf, not echo). The stub
// includes the project prompt name so the agent knows which canopy prompt it would have
// received. DagmarMixins are not rendered (deferred — see package doc).
//
// FUTURE: when `cn` is provisioned in the pod, this command will cd into workspaceDir/.canopy
// and render the project prompt via canopy. Full cross-store merge (Compose + DagmarMixins)
// is deferred.
func ShellComposeCommand(projectPrompt string, workspaceDir string, outputPath string) string {
	// Attempt canopy render first; fall back to a functional stub with real newlines.
	// Uses printf (not echo) so \n produces actual newlines in busybox.
	return fmt.Sprintf(
		`cd %s && cn render %s --format text > %s 2>/dev/null || printf '%%s\n' '# Agent Prompt' '' 'Project prompt (canopy not available): %s' 'See the project module'"'"'s prompt documentation for full instructions.' > %s`,
		workspaceDir, projectPrompt, outputPath, projectPrompt, outputPath,
	)
}
