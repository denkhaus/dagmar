# Canopy prompt model — spike findings

- **Date:** 2026-07-31
- **Canopy version:** `@os-eco/canopy-cli` 0.2.6 (CLI `cn`, bun-installed)
- **Purpose:** learn canopy's native data model so dagmar's `Prompt` CRD and
  `ProjectManifest` require **zero/minimal conversion** (seeds `dagmar-8097`, B3).
- **Method:** hands-on spike (`/tmp/canopy-spike`) + `cn prime` + README.

## Native data model

Storage is git-native JSONL (`.canopy/`), identical pattern to seeds/mulch
(`merge=union` gitattribute → branch merges just work):

- **`.canopy/prompts.jsonl`** — one prompt record per line:
  `{id, name, version, sections:[{name, body}], status, createdAt, updatedAt,
  tags[], frontmatter{}, extends?, mixins[], schema?}`.
- **`.canopy/schemas.jsonl`** — `{id, name, requiredSections[], optionalSections[]}`.
- **`.canopy/config.yaml`** — `{project, version, targets{}}`; `targets` are named
  emit directories (default `agents/`). A JSON Schema is published for external UIs.

## Composition model (this is the enrichment mechanism)

Prompts are **composed, not duplicated**:

- **`extends`** — single inheritance from one parent.
- **`mixins`** — multi-inheritance (list); horizontal reuse.
- **Sections** — a prompt is a set of named `{name, body}` sections; a child can
  **override / append / remove** individual sections. Up to **5 levels deep**, with
  circular-reference detection.
- **Resolve order** (last wins on section-name conflict): `extends` chain (parent first,
  recursive) → `mixins` → self.

Verified: child `project-coder extends base-coder` + mixin `review-format`, overriding
the `rules` section → `cn render` produces the merged section set, `resolvedFrom:
[base-coder, review-format, project-coder]`.

## No variable templating

Confirmed by grep of README + CLI: **no `{{var}}`, no parameter/placeholder/injection.**
Enrichment is exclusively **section composition + frontmatter + inheritance**, never
data-substitution. (If runtime values are needed, they are emitted as frontmatter or
sections, not interpolated.)

## emit → Dagger bridge (zero conversion)

`cn emit <name>` renders the resolved prompt and writes a plain `.md` file
(`agents/<name>.md`, or custom `--emit-dir` / `--emit-as`; `.ts` module if the emit name
ends in `.ts`). The emitted file is YAML frontmatter + `## section` bodies — **exactly
what `dag.LLM().WithPromptFile(...)` consumes.** dagmar needs no prompt-format conversion
layer; it shells to `cn emit`/`cn render` and passes the file.

## Other capabilities

- **Versioning:** every `cn update` creates a new version; `history`, section-aware
  `diff <v1> <v2>`, and `pin <name>@<version>` for reproducibility.
- **Validation:** `schema` (required/optional sections) + `cn validate`; a schema is
  assigned per prompt.
- **Frontmatter:** arbitrary `--fm key=value` (repeatable) — the per-prompt config
  channel (e.g. `model`).
- **Mulch integration:** prompts declare expertise deps via frontmatter
  (`mulch.prime.domains[]`, `mulch.prime.files[]`, `mulch.budget`, `mulch.on_empty`,
  `extends_mulch`). Canopy surfaces the resolved `mulch` field in `render --format json`
  (omitted entirely when none declared) and **never shells to `ml`** — the consumer
  (dagmar) reads it and runs `ml prime` itself. Directly serves the Memory port.
- **Concurrency:** advisory file locks + atomic writes → multi-agent safe.

## Decision — cross-store composition (Variant A, ADR-0005)

Canopy stores are **self-contained**: `extends`/`mixin` resolve only **within the same
`.canopy/`**; `cn render`/`emit` have no ad-hoc override (`--mixin` is rejected); there
is no registry or cross-store inheritance; `cn import` copies a local `.md` as a fresh
prompt (not an `extends` link). So canopy cannot natively merge across stores.

**Constraint:** dagmar's prompts must NOT be copied into a project's store; dagmar must
control the final emitted output.

**Decision (Variant A):** dagmar performs the cross-store composition itself —
`cn render <project-prompt> --format json` (project store) + `cn render <dagmar-mixin>
--format json` (dagmar store), then merges the section sets in a thin Go layer (canopy
rules: parent → mixins → self, same-name last-wins), transforms/validates, and writes
the final `.md` for `WithPromptFile`. Stores stay cleanly separated; dagmar owns the
output. See **ADR-0005**.

Rejected alternatives: vendor dagmar prompts into the project store (pollutes it);
transient merged store (more plumbing, transiently copies dagmar prompts); native
canopy cross-store mixin (not available).

## Implications for dagmar

- **B3 (ProjectManifest prompt format):** **no bespoke prompt-enrichment JSON field.**
  Per-project prompt customization is native canopy work — child prompts in the project's
  `.canopy/` that `extends` the bases and override sections / set frontmatter. The
  `ProjectManifest` only needs to **bind the project's canopy store** (already the
  os-eco-Binding field, ADR-0003). → removes the "prompt-enrichment JSON" line from
  CONTEXT.md / ADR-0003.
- **D6 (Prompt CRD) refinement:** the `Prompt` CRD is a **reference to a canopy prompt**
  (`name`, optional `@version` pin), not a dagmar-invented composition-spec. canopy does
  all composition; dagmar resolves the CR → `cn render`/`emit` → `.md` →
  `WithPromptFile`. (Corollary worth noting: since the "spec" is just a canopy-prompt
  reference, `Prompt` could alternatively be a field on the `Agent` CR
  (`canopyPrompt: project-coder@2`) rather than a standalone CRD — a D5 reconsideration
  if desired.)
- **Cross-store composition** — decided: **Variant A**, dagmar-side merge (ADR-0005).

## Reference

- CLI: `cn` (`@os-eco/canopy-cli@0.2.6`); `cn prime` for agent workflow context.
- Spike artifacts: `/tmp/canopy-spike/.canopy/` (base-coder, review-format,
  project-coder, agent-schema; v1/v2, pin, mulch-via-fm).
