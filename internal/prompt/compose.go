// Package prompt implements dagmar's cross-store prompt composition (ADR-0005, Variant A).
//
// The agent pod provisions canopy CLI (cn) via bun at startup, then ShellComposeCommand
// renders the project prompt + DagmarMixins, merges section sets following canopy's
// resolution rules (parent/mixins first, self last; same-name = last wins), and writes
// the final markdown to /tmp/prompt.md before the code() call.
//
// Compose() is the Go reference implementation of the section merge — unit-tested and
// available for future controller-side composition. The pod-side shell command (via jq)
// replicates its merge semantics at runtime.
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
// STATUS: Go reference implementation of the merge. Unit-tested. The pod-side
// ShellComposeCommand replicates this logic via jq at runtime (same semantics:
// mixins first, project last, same-name = last wins).
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

// ShellRenderCommand builds the shell command to render a single canopy prompt to a .md file
// inside a container. Strips the canopy header lines (name, version, resolved-from) so
// only the ## section bodies remain.
func ShellRenderCommand(promptName string, canopyDir string, outputPath string) string {
	return fmt.Sprintf(
		`cd %s && cn render %s --format md 2>/dev/null | awk '/^## /{p=1} p' > %s`,
		canopyDir, promptName, outputPath,
	)
}

// ShellComposeCommand builds a shell command that performs the ADR-0005 cross-store
// composition inside the agent pod. It renders the project prompt and each DagmarMixin
// via canopy CLI (cn), merges the section sets following canopy's resolution rules
// (parent/mixins first, self last; same-name section = last wins), and writes the final
// markdown to outputPath.
//
// The merge uses jq to deduplicate sections by name (matching Compose() semantics):
// all section objects are collected (mixins first, project last), reduced into a
// name→body map (last wins), then emitted as ## section markdown.
//
// If cn is not available (not yet provisioned), the command falls back to a functional
// stub prompt so the agent pod can still proceed (Phase 0 resilience).
func ShellComposeCommand(projectPrompt string, dagmarMixins []string, workspaceDir string, outputPath string) string {
	// Build section-collecting subcommands: mixins first (lower priority),
	// then the project prompt (highest priority — overrides same-named sections).
	// Each renders as JSON and extracts individual section objects via jq.
	var renders []string
	for _, m := range dagmarMixins {
		renders = append(renders, fmt.Sprintf(
			`cn render %s --format json 2>/dev/null | jq -c '.sections[]'`,
			m,
		))
	}
	renders = append(renders, fmt.Sprintf(
		`cn render %s --format json 2>/dev/null | jq -c '.sections[]'`,
		projectPrompt,
	))
	allRenders := strings.Join(renders, "; ")

	// Merge: pipe all section JSONs through jq to dedup by name (last wins)
	// and emit as ## section markdown — exactly what Compose() produces.
	mergeJq := `jq -rs 'reduce .[] as $s ({}; .[$s.name]=$s.body) | to_entries[] | "## \(.key)\n\n\(.value)\n"'`

	return fmt.Sprintf(
		`cd %s && { %s } | %s > %s 2>/dev/null || printf '%%s\n' '# Agent Prompt' '' 'Project prompt (canopy not available): %s' 'See the project module'"'"'s prompt documentation for full instructions.' > %s`,
		workspaceDir, allRenders, mergeJq, outputPath, projectPrompt, outputPath,
	)
}
