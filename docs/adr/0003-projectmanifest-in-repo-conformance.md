# ADR-0003: Project conformance via in-repo ProjectManifest

- **Status:** decided
- **Date:** 2026-07-31
- **Resolved in:** seeds dagmar-4271

## Context

Each Project has **project-specific** entities: its own `checkables` (build/test/lint),
its os-eco stores (seeds/mulch/canopy), and its prompt-enrichment JSON. For dagmar to
operate uniformly on *any* project that follows the spec, these entities must be
**standardized** behind a conformance contract. The open question is where that
contract lives.

## Decision

**ProjectManifest** is an **in-repo** conformance contract: a well-known manifest
(analogous to `.seeds/` / `.mulch/` / `.dagger/`) that each Project exposes, containing
its project-specific `checkables`, os-eco binding (incl. the project's `.canopy/` store
path), and repo/flow metadata. (Prompts themselves live in the project's `.canopy/`
store — see ADR-0005; the manifest only points at it.) It is:

- **git-native**, versioned with the code it describes;
- **referenced** by the `Project` CR (repo + path), which does **not** duplicate the
  project-specific content (the CR may mirror fields read-only as status for
  observability);
- **not a CRD** and **not declared on the Project CR**.

The contract **grows via dogfooding**: dagmar (itself a Project) defines its own
ProjectManifest first; each capability dagmar gains can extend the standard.

## Alternatives considered

- **Declare checkables etc. on the `Project` CR.** Rejected — decouples verification
  config from the code it verifies, causing drift between the cluster and the repo.
- **ProjectManifest as its own CRD.** Rejected — it is git-native and must travel with
  the code; making it a K8s resource would split the source of truth.
- **Hardcode per-project behaviour in dagmar.** Rejected — violates the goal of
  operating on any conforming project.

## Consequences

- Projects self-describe; dagmar reads the manifest at runtime when it clones the repo
  for a Workspace, so it never hard-codes a project's idiosyncrasies.
- The `checkable` chain is unified: `ProjectManifest` declares project-specific
  checkables → the `Workspace` carries them → `CodeWorkspace` (Tier A) executes them.
- The conformance spec is evolvable and bootstrapped by dagmar developing itself.
