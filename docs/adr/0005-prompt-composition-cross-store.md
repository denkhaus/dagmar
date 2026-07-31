# ADR-0005: Prompt composition — dagmar-side cross-store merge (Variant A)

- **Status:** decided
- **Date:** 2026-07-31
- **Resolved in:** seeds `dagmar-8097` (ProjectManifest spec) + canopy spike
  (`docs/research/canopy-prompt-model.md`)

## Context

Each Project holds its own canopy store (`.canopy/`) with **project-content** prompts
(the project's role/task/domain specifics, mulch deps). dagmar holds **operational**
prompts (output-format, review-gating, safety/autonomy-bounds, tool-rules) in its own,
dogfooded canopy store. An Agent's final prompt must combine both.

Canopy stores are **self-contained**: `extends`/`mixin` resolve only within one store;
`cn render`/`emit` have no ad-hoc override (`--mixin` is rejected); there is no registry
or cross-store inheritance. So canopy cannot natively merge across the two stores.

**Constraint (user):** dagmar's prompts must NOT be copied into a project's store — the
project store stays project-only — and dagmar must control the final emitted output.

## Decision

dagmar performs the cross-store composition itself (**Variant A**):

1. `cn render <project-prompt> --format json` (project store) → project sections.
2. `cn render <dagmar-mixin> --format json` (dagmar store) → dagmar operational sections.
3. dagmar merges both section sets in a thin Go layer, following canopy's resolution
   rules (parent → mixins → self; same-name section = last wins), applies any
   transforms/validation, and writes the final `.md` passed to
   `dag.LLM().WithPromptFile(...)`.

The two stores stay cleanly separated; dagmar owns the final output.

## Alternatives considered

- **Vendor dagmar prompts into the project store at bootstrap.** Rejected — pollutes the
  project's `.canopy/` with dagmar-owned prompts; violates the clean-store /
  project-autonomy constraint.
- **Transient merged store (Variant B).** Rejected — lets canopy do all merging, but
  requires assembling/rewriting a throwaway `.canopy/` per render and copies dagmar
  prompts transiently; more plumbing for no gain over a ~30-line Go merge.
- **Native canopy cross-store / ad-hoc mixin.** Not available (verified: `--mixin` is an
  unknown option; mixins are stored-record-only). If canopy adds it later, dagmar drops
  the Go merge layer.

## Consequences

- Project `.canopy/` holds only project-content prompts; dagmar `.canopy/` holds
  operational mixins — both evolve independently.
- The Agent prompt binding = (project prompt ref) + (dagmar mixin refs); dagmar composes
  at run time.
- dagmar carries a small, well-defined section-merge responsibility (canopy's rules);
  documented as part of the CRD→Dagger bridge in `CONTEXT.md`.
- The `Prompt` CRD is a **reference to canopy prompts**, not a dagmar-invented spec
  (refines the earlier D6 framing).
