# ADR-0020: Workspace & repository interaction model

- **Status:** decided
- **Date:** 2026-08-09
- **Resolved in:** seed dagmar-e1f1 + this ADR
- **Evidence:** grilling session 2026-08-09; builds on ADR-0007 (credentials), ADR-0008
  (engine tenancy/cache), ADR-0011 (hermeticity), ADR-0012 §4 (gate-family), ADR-0009
  (quality gate), ADR-0013 (control plane).
- **Updates:** CONTEXT.md Workspace/CodeWorkspace glossary (v0.21.8 API reconciliation).

## Context

Phase 2 (cognition) needs a concrete workspace model: how dagmar clones a project repo,
isolates it per Run, hands it to the LLM Loop, and extracts the diff for a PR. CONTEXT.md
specifies the concept ("task-scoped, Run-isolated clone, strict isolation per Run, lineage
across Task's Runs, final output → diff → PR") but not the mechanism.

A critical empirical finding from the v0.21.8 SDK: **`CodeWorkspace(source, checkable)` does not
exist in v0.21.8.** The API surface is:

- `dag.CurrentWorkspace()` → `*Workspace` (the workspace the current module call runs in)
- `Env.WithWorkspace(workspace *Directory)` → `*Env` (workspace is a `*Directory`)
- `Env.Checks(opts)` → `*CheckGroup` (check functions from installed modules; `.Run()`)
- `Workspace.Update()` → `*Changeset` (`.AsPatch()`, `.Before()`, `.After()`, `.DiffStats()`)
- `dag.LLM(opts)` with `LLMOpts{Model string, MaxAPICalls int}`

This means the source is a Directory passed to the Env, the checkable is module-annotated check
functions discovered by the Env, and the diff is a Changeset from Workspace.Update(). The
"context.md glossary's `CodeWorkspace(source, checkable)` reflects a conceptual (pre-API) design;
this ADR reconciles it with the actual v0.21.8 surface.

## Decision

### D1 — Clone = ephemeral Dagger Directory (no persistent cache volume)

The workspace clone is an **ephemeral `*dagger.Directory`** built fresh per Run via
`dag.Git(repoURL).Branch(branchName).Tree()`. It is not persisted on a cache volume — the
Dagger engine content-addresses the git fetch, so cloning the same ref is a cache hit at the
engine level. The clone is handed to `env.WithWorkspace(source)`; the post-Loop
`workspace.Update()` captures the agent's changes as a Changeset. No explicit GC: ephemeral
Directories are managed by the engine's cache lifecycle.

**Rationale:** ADR-0008 isolates cache by volume name for *cache volumes* (mutable, persisted).
A git clone tree is immutable (content-addressed) — it does not need a named volume. The
engine's git fetch cache provides the "clone once, reuse" property without dagmar managing
volume lifecycle.

### D2 — Branching: `dagmar/<run-name>` from the Project's default branch

Each Run clones from the Project's default branch (`main` unless overridden) and works on a
branch named `dagmar/<run-name>` (e.g. `dagmar/feature-add-tests-x7y2`). The branch is created
inside the agent pod via git CLI before the Loop starts. If the branch already exists (lineage
Run-in from a prior Run-out), the clone includes the prior Run's branch head.

**Base ref:** the Project's `spec.defaultBranch` (default: `main`), resolved at clone time.
**Force-push:** never. If push fails (branch moved), the Run fails with a clear error
(conflict → re-clone from base, re-attempt). This is rare given per-Run isolation.

### D3 — PR flow: controller action, post-Loop, via GitHub API

The PR is **not** created by the LLM inside the Loop. It is a **controller action** after the
Loop completes and `dagmar-gate` passes (two-green model, ADR-0006). The controller:

1. Extracts the Changeset from the completed Loop's workspace (`.AsPatch()` or `.After()`)
2. Pushes the branch to the remote (`vcs` push credential, ADR-0007)
3. Creates the PR via `gh` CLI or GitHub API: title from the Task/seeds issue, body with
   Task description + changeset summary

**PR metadata:** title = Task title (from the seeds issue); body = Task description + Run
summary + gate output. The agent never holds push authority (ADR-0006 §D3, ADR-0007 §5) — the
controller exercises the push credential at controller level.

### D4 — Single-repo per Task (no multi-repo fan-out in v1)

A Task touches exactly one repository (the Project's repo or its fork). Multi-repo fan-out
requires a workflow abstraction (ADR-0016) that is not needed for Phase 2. If a future workflow
needs cross-repo changes, each repo gets its own Run within the workflow's orchestration.

### D5 — Credentials: `vcs` class projected pod-side for clone, controller-side for push

Per ADR-0007 §5:

- **Clone (read):** the `vcs` credential (PAT) is projected into the agent pod as an env var.
  The pod's git clone uses it via a credential helper (same pattern as the existing
  `GitCredentialsRef` in Phase 0). This is **read-only** clone authority.
- **Push:** the controller exercises the `vcs` push credential **outside** the agent pod
  (controller-level, ADR-0006 §D3). Agents never hold push authority. The push happens
  post-Loop, post-gate, as part of D3's PR flow.

This preserves the ADR-0007 invariant: "the merge tool is in no Agent's tool-set. Merge is a
deterministic controller function."

### D6 — Checkable = project module check functions via Env.Checks()

The checkable is **not** a separate argument or a manual function call. In v0.21.8, check
functions are module-annotated (`// dag check` annotation or `dag.Check*` constructors in the
module). `env.Checks()` discovers them from the installed modules. The project's gate logic
(ADR-0017 §3: checkables in `dagmar-gate` code) is expressed as check functions the Env discovers.

**In-loop self-verification:** the agent calls `env.Checks().Run()` during the Loop — this runs
the project's check functions (gate logic) on the workspace. The LLM sees the check results and
can iterate. This is ADR-0009's "gate is the in-loop self-verification" realized through the
native check mechanism.

**Relationship to `dagmar-gate` function:** `dagmar-gate` (the existing Phase 1 function) remains
the CI/gate-family entry point (`dagger call -m .dagmar dagmar-gate`). In-loop, the gate logic
is exposed as **check functions** that the Env discovers. This may mean refactoring shared gate
logic into check functions + a `dagmar-gate` dispatcher (Phase 2 implementation detail), or
`dagmar-gate` itself becoming a check function.

### D7 — Workspace lineage: branch-based, not volume-based

Workspace lineage (Run-out → next Run-in, CONTEXT.md) is realized through **git branches**, not
persisted volumes. A failed/revised Run's output lives on its branch
(`dagmar/<run-name>`); the next Run in the Task clones from that branch head (not the default
branch). This is simple (no volume bookkeeping), recoverable (branches are inspectable), and
consistent with the ephemeral-directory model (D1).

## v0.21.8 API reconciliation (CONTEXT.md glossary update)

The CONTEXT.md glossary defines:

> **CodeWorkspace** — `CodeWorkspace(source, checkable)`; the Tier-A projection of a
> dagmar Workspace.

This does not match v0.21.8. The actual surface is `Env.WithWorkspace(*Directory)` +
`Env.Checks()`. The glossary **was updated** in this same commit range — see CONTEXT.md Workspace/checkable entries.

## Consequences

- **No cache volume lifecycle:** ephemeral Directories + engine git-fetch cache = no volume
  management for workspace clones. Per-Project cache volumes (ADR-0008) remain for build caches
  (`~/.cache/go-build`, etc.), not for source clones.
- **Branch hygiene:** dagmar creates and pushes `dagmar/*` branches; the controller cleans up
  merged/stale branches as a housekeeping action (Phase 3 cron).
- **PR creation is controller-side:** agents produce code + pass the gate; the controller pushes
  and opens the PR. This is the two-green model (ADR-0006) applied to the workspace lifecycle.
- **Check functions are the in-loop gate:** the project's gate logic must be expressible as
  Dagger check functions for in-loop self-verification. `dagmar-gate` may need refactoring
  (Phase 2 implementation).
- **Single-repo v1:** no cross-repo Tasks until a workflow needs them.
- **CONTEXT.md glossary update needed:** CodeWorkspace → Workspace, reflecting v0.21.8 API.

## Alternatives considered

- **Persistent cache volume per workspace (ADR-0008 pattern).** Rejected for clones — a git tree
  is immutable; the engine content-addresses the fetch. A named volume adds lifecycle management
  (allocation, GC, name collision) for no benefit over the engine's native git cache.

- **Clone inside the Loop (LLM does the git clone).** Rejected — the clone is infrastructure
  (credentials, branch creation), not cognition. dagmar prepares the workspace before the Loop
  starts; the LLM operates on a ready workspace.

- **PR created by the agent (in-Loop tool).** Rejected — violates ADR-0006/0007 (agents never
  hold push authority). The controller is the sole pusher.

- **Multi-repo Tasks in v1.** Rejected — adds workflow orchestration complexity before a single
  coder-loop has proven itself. Deferred to Phase 3.

- **Workspace lineage via persisted volumes.** Rejected — branch-based lineage is simpler,
  inspectable, and recoverable. Volumes require bookkeeping and are opaque to humans.
