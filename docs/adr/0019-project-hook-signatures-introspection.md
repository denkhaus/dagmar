# ADR-0019: Project Hook function signatures + introspection conformance

- **Status:** decided (`dagmar-prompt` hook D2/D3 removed — superseded by ADR-0023 Prompter-LLM)
- **Date:** 2026-08-09
- **Resolved in:** seed dagmar-c712 + this ADR
- **Evidence:** builds on ADR-0017 (hook registry), ADR-0018 (port layer removed), ADR-0005
  (cross-store merge), ADR-0009 §2 (gate family), ADR-0011 (hermeticity), ADR-0012 §4
  (always-Dagger gate-family), ADR-0014 (module boundary).
- **Supersedes:** ADR-0017 §8 (dagmar-prompt asymmetry deferred to "ADR-0018" — now resolved here).

## Context

ADR-0017 established the five Project Hooks as named Dagger module functions in two categories
(programmatic + LLM-Tool), and ADR-0018 removed the Go port layer. ADR-0017 §8 deferred the
exact function signatures and the `dagmar-prompt` relationship to ADR-0005's cross-store merge
to a follow-up ADR. This is that follow-up.

Three open questions:

1. **Programmatic hook signatures** — `dagmar-bootstrap` and `dagmar-gate` are implemented and
   working. Confirm their contract.
2. **LLM-Tool hook signatures** — `dagmar-issues`, `dagmar-memory`, `dagmar-prompt` are not yet
   implemented. Design their function contracts.
3. **`dagmar-prompt` asymmetry** — ADR-0005's cross-store merge is dagmar-side Go logic. Does
   `dagmar-prompt` replace, wrap, or supplement it?
4. **Conformance check** — how does dagmar verify a project module exposes all required hooks?

## Decision

### D1 — Programmatic hooks (confirmed, already working)

Programmatic hooks are called by dagmar's controller (or CI/lefthook) via
`dagger call -m <ref>`. They take the project source explicitly (the controller passes it) and
a credentials secret (ADR-0007).

```go
func (m *Dagmar) DagmarBootstrap(
    ctx context.Context,
    source      *dagger.Directory, // project source tree
    githubToken *dagger.Secret,     // +optional — ADR-0007 credential injection
) (string, error)

func (m *Dagmar) DagmarGate(
    ctx context.Context,
    source      *dagger.Directory, // project source tree
    githubToken *dagger.Secret,     // +optional — ADR-0007 credential injection
) (string, error)
```

**Contract:** `source` is the project source tree (a `*dagger.Directory`); `githubToken` is
optional (nil = unauthenticated). Returns a human-readable summary string; error = failure
(non-zero exit). `dagmar-gate` aborts on the first failing checkable.

These signatures are **confirmed as-is** — the current `.dagmar/main.go` implementation already
matches. No change required.

### D2 — LLM-Tool hooks (designed)

LLM-Tool hooks are registered via `Env.WithMainModule()` and called by the LLM as native Dagger
tools during the loop. They operate on the project worktree (`.seeds/`, `.mulch/`, `.canopy/`)
**implicitly** — no `source` parameter. The workspace source is accessible inside the module via
`dag.CurrentModule()` (`.Source()` / `.Workdir(path)`). The LLM passes only operation-specific
arguments.

**`dagmar-issues`** — CRUD on the project's issue tracker (seeds, Linear, etc.):

```go
func (m *Dagmar) DagmarIssues(
    ctx context.Context,
    // action: "read" | "search" | "create" | "update"
    action string,
    // id: issue identifier (for read/update)
    // +optional
    id string,
    // query: search text (for search)
    // +optional
    query string,
    // title: issue title (for create)
    // +optional
    title string,
    // body: issue body (for create/update)
    // +optional
    body string,
) (string, error)
```

Returns: issue details (read), search results (search), new issue id (create), or confirmation
(update). The hook formats output for LLM consumption (JSON-as-string is fine). A project that
lacks issue tracking implements this as a noop (`read` returns empty, `create`/`update` return
"noop").

**`dagmar-memory`** — read/write project expertise (mulch):

```go
func (m *Dagmar) DagmarMemory(
    ctx context.Context,
    // action: "read" | "search" | "write"
    action string,
    // query: expertise query or domain (for read/search)
    // +optional
    query string,
    // key: record key/domain (for write)
    // +optional
    key string,
    // value: expertise content (for write)
    // +optional
    value string,
) (string, error)
```

Returns: expertise content (read), search results (search), or confirmation (write). A project
without memory implements this as a noop (`read` returns empty).

**`dagmar-prompt`** — render project prompt content from canopy at runtime:

```go
func (m *Dagmar) DagmarPrompt(
    ctx context.Context,
    // name: the canopy prompt name to render (resolved via cn render)
    name string,
) (string, error)
```

Returns: the rendered prompt content (canopy sections as `.md`). A project without canopy returns
an empty string (noop).

**Error contract for all LLM-Tool hooks:** error = operational failure (e.g., corrupt store, IO
error). Noop implementations return valid empty results, **never errors** — a noop is a
successful empty response, not a failure.

### D3 — `dagmar-prompt` supplements the ADR-0005 cross-store merge (asymmetry resolved)

The cross-store merge (ADR-0005) and `dagmar-prompt` serve **different purposes at different
times**. They are not in conflict:

| Aspect | ADR-0005 cross-store merge | `dagmar-prompt` hook |
|--------|---------------------------|----------------------|
| **Caller** | Controller (pre-loop) | LLM (in-loop) |
| **When** | Before `Loop()` — builds the initial `.md` for `WithPromptFile` | During `Loop()` — on-demand |
| **What** | Merges dagmar operational mixins ⊕ project content prompts | Renders a single named prompt from the project's `.canopy/` store |
| **Scope** | dagmar-side Go logic (both stores) | Project module function (project store only) |

**The merge is NOT replaced.** dagmar retains its ADR-0005 controller-side merge as the sole
composer of the agent's initial prompt. This is critical for safety: dagmar must control its
operational mixins (output format, review gating, safety bounds, tool rules) — a project hook
cannot omit them.

**`dagmar-prompt` supplements the merge.** It is a runtime capability — the LLM can call it to
fetch additional project prompt content during the loop (e.g., project-specific review checklists,
testing guidelines, commit message format). It renders from the project's `.canopy/` store only;
dagmar operational content is already in the initial prompt.

This resolves the ADR-0017 §8 asymmetry cleanly:
- `dagmar-issues` and `dagmar-memory` are thin delegations (hook IS the implementation).
- `dagmar-prompt` is NOT a thin delegation of the merge — it is a **separate, supplementary**
  tool. The merge remains dagmar-side Go logic; the hook is a runtime convenience for the LLM.

### D4 — Conformance check via Dagger introspection

dagmar verifies that a project module exposes all required hooks **before a Run starts** (fail
fast). The check uses Dagger's introspection API (`Module.Objects(ctx)` → `ObjectTypeDef.Functions(ctx)`,
not a non-existent `Module.Functions()` — Review 26 A3 correction):

```go
// Load the project module from source
src := dag.ModuleSource(projectModuleRef)  // e.g., ".dagmar"
mod := src.AsModule()
mod, err := mod.Sync(ctx)  // force load + validate

// Enumerate functions: Module has no Functions() method in v0.21.8.
// The path is: mod.Objects(ctx) → []TypeDef → each AsObject().Functions(ctx)
hookFns := make(map[string]*dagger.Function)
objects, err := mod.Objects(ctx)
for _, td := range objects {
    obj := td.AsObject()
    fns, err := obj.Functions(ctx)
    for _, fn := range fns {
        name, _ := fn.Name(ctx)  // kebab-case, e.g. "dagmar-issues"
        hookFns[name] = &fn
    }
}

// Check required hooks
required := []string{"dagmar-bootstrap", "dagmar-gate"}
if runInvolvesLLM {
    required = append(required, "dagmar-issues", "dagmar-memory", "dagmar-prompt")
}
for _, req := range required {
    if _, ok := hookFns[req]; !ok {
        return fmt.Errorf("conformance: required hook %q not found in project module", req)
    }
}
```

**Two-tier conformance:**

- **Programmatic hooks** (`dagmar-bootstrap`, `dagmar-gate`): **always required.** A project
  without them cannot run through the gate family.
- **LLM-Tool hooks** (`dagmar-issues`, `dagmar-memory`, `dagmar-prompt`): **required when a Run
  involves an LLM agent** (Phase 2+). A gate-only Run (no LLM) does not require them. A noop
  implementation satisfies the check (the function exists, even if it returns empty).

**Parameter introspection (Phase 3, deferred):** `Function.Args(ctx)` → `[]FunctionArg`, each
with `Name(ctx)` and `TypeDef().Kind(ctx)` (STRING_KIND, INTEGER_KIND, BOOLEAN_KIND, OBJECT_KIND,
…). Phase 2 conformance is name-presence only; Phase 3 adds parameter-name and type-kind
verification for forward compatibility and better error messages.

**What is NOT checked:** return types, internal logic, or backing-service identity. Conformance
verifies the contract surface (function names, later parameter names/types), not implementation.

## Alternatives considered

### For LLM-Tool hook signatures

- **Multiple functions per hook** (`dagmar-issues-read`, `dagmar-issues-create`, …). Rejected —
  better individual LLM tool descriptions, but breaks ADR-0017's one-function-per-hook naming
  convention and complicates conformance checking (must verify a group of names, not one).
  Internal action-dispatch in a single function gives the same flexibility with simpler
  conformance.

- **Structured input type per hook** (a `DagmarIssuesInput` struct arg). Rejected — Dagger's
  v0.21.8 codegen rejects custom struct types from non-main packages as function args
  (ADR-0010 empirical constraint), and main-package structs add codegen complexity. Primitive
  string args with `+optional` tags are universal and LLM-friendly.

- **`source *dagger.Directory` as explicit parameter on LLM-Tool hooks.** Rejected — the LLM
  does not naturally hold a directory reference to pass; the workspace is implicit in the module
  context (`dag.CurrentModule()`). Forcing the LLM to pass source adds friction with no benefit.

### For the dagmar-prompt asymmetry

- **Replace (dagmar-prompt IS the sole prompt composer).** Rejected — violates ADR-0005's
  constraint that dagmar controls operational mixins. A project could omit safety rules from its
  hook.

- **Wrap (dagmar calls dagmar-prompt, then layers its mixins on top).** Rejected — needlessly
  couples the controller's merge to a project function call. The merge is simpler as direct Go
  logic reading both canopy stores. The project hook adds latency and a failure mode to a path
  that doesn't need it.

- **Merge dagmar-prompt into dagmar-memory.** Rejected — canopy (structured prompt templates with
  section inheritance) and mulch (freeform expertise records) are different data models with
  different tools (`cn` vs `ml`). Merging them loses the distinction.

### For conformance checking

- **Compile-time Go interface conformance (the deleted port layer).** Rejected — this IS what
  ADR-0018 removed. Dagger modules are dynamically loaded; introspection is the natural fit.
- **Manifest-declared hook registry (YAML).** Rejected — ADR-0017 §4 slimmed the manifest to
  metadata. Declaring hooks in YAML would recreate the "second source" problem ADR-0017 §3
  resolved for checkables.

## Consequences

- **Five hook contracts are now fully specified** — two programmatic (confirmed), three LLM-Tool
  (designed). The `.dagmar/` project module grows three LLM-Tool functions in Phase 2.
- **Noop pattern is the conformance escape hatch** — a project that lacks a capability implements
  the function as a noop, satisfying the name-presence check. This keeps the LLM's tool-surface
  predictable (ADR-0017 §2).
- **ADR-0005 is unaffected** — the cross-store merge remains dagmar-side controller logic. The
  `dagmar-prompt` hook is purely additive.
- **Conformance is introspection-based and name-keyed** — simple to implement, easy to extend to
  parameter checking. No Go interface package, no manifest declarations.
- **Workspace access for LLM-Tool hooks is implicit** — via `dag.CurrentModule()`. The exact
  access path (Source() vs Workdir()) is resolved in Phase 2 implementation; the design principle
  is that `.seeds/`, `.mulch/`, `.canopy/` are accessible from within module functions without an
  explicit `source` argument.
- **ADR-0017 §8 asymmetry is resolved** — `dagmar-prompt` supplements (not replaces/wraps) the
  ADR-0005 merge.
