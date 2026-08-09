// Package prompt implements dagmar's cross-store prompt composition (ADR-0005, Variant A).
//
// The controller composes the agent's final prompt before dispatching the code() function.
// It renders the project-content prompt from the project's .canopy/ store and dagmar
// operational mixins from dagmar's own .canopy/ store, merges them per canopy's resolution
// rules (parent → mixins → self; same-name section = last wins), and writes the final .md
// passed to WithPromptFile.
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
	// Collect names
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	// Simple sort — section order within the prompt is alphabetical for MVP.
	// A future refinement could preserve canopy's resolvedFrom order.
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
// inside a container. This is used by the controller to construct the agent pod's command.
//
// `cn render <name>` renders the resolved prompt (extends + mixins resolved) and writes
// to stdout. `--format text` produces plain markdown. The command writes to the given
// output path.
func ShellRenderCommand(promptName string, canopyDir string, outputPath string) string {
	return fmt.Sprintf(
		`cd %s && cn render %s --format text > %s`,
		canopyDir, promptName, outputPath,
	)
}

// ShellComposeCommand builds a shell command sequence that renders the project prompt
// and writes it to outputPath. For MVP, this renders only the project prompt (dagmar
// operational mixins are deferred — the project prompt is the load-bearing content).
//
// The project's .canopy/ store lives in the cloned workspace at /workspace/.canopy/.
// canopy CLI (`cn`) must be installed in the pod.
func ShellComposeCommand(projectPrompt string, workspaceDir string, outputPath string) string {
	// For MVP: render the project prompt only. Full cross-store merge (dagmar mixins)
	// requires access to dagmar's .canopy/ store, which the pod doesn't have yet.
	// The dagmar-prompt in-loop hook (ADR-0019) provides on-demand project prompt
	// access during the Loop.
	_ = workspaceDir
	return fmt.Sprintf(
		`cn render %s --format text > %s 2>/dev/null || echo "# Agent Prompt\\n\\nProject prompt: %s" > %s`,
		projectPrompt, outputPath, projectPrompt, outputPath,
	)
}
