// Package oseco will hold the os-eco adapter implementations: concrete IssueTracker,
// Memory, and Prompts (ports) backed by the seeds (sd), mulch (ml), and canopy (cn) CLIs
// invoked from within Dagger containers (ADR-0001 Tier B; ADR-0010 §3). Implementations
// land here as the real Run flow develops.
package oseco
