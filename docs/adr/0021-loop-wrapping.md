# ADR-0021: Loop-wrapping — how dagmar drives dag.LLM().Loop()

- **Status:** decided
- **Date:** 2026-08-09
- **Resolved in:** seed dagmar-8d4a + this ADR
- **Evidence:** grilling session 2026-08-09; builds on ADR-0005 (prompt composition),
  ADR-0009 (quality gate), ADR-0010 (Go module layout), ADR-0011 (hermeticity),
  ADR-0016 (dual-mode Run), ADR-0017/0019 (Project Hooks), ADR-0020 (workspace model).
- **Updates:** CONTEXT.md Loop/Env glossary entries (v0.21.8 reconciliation).

## Context

Phase 2 (cognition) needs a concrete design for how dagmar builds the Dagger `Env` and drives
`dag.LLM().Loop()`. CONTEXT.md specifies the concept ("a Run drives exactly one Loop; dagmar
does not reimplement cognition") and ADR-0020 specified the workspace (source Directory +
env.Checks()). This ADR specifies the Env construction, the loop-driving method, multi-step task
handling, and token/cost management.

### v0.21.8 API surface (verified from generated SDK)

```go
// LLM
dag.LLM(LLMOpts{Model string, MaxAPICalls int}) → *LLM
llm.WithEnv(env *Env) → *LLM
llm.WithPromptFile(file *File) → *LLM
llm.WithModel(model string) → *LLM
llm.Loop() → *LLM          // blocks until the agent is done
llm.Step() → (*LLM, error)  // single step (non-blocking alternative)
llm.TokenUsage() → *LLMTokenUsage
llm.LastReply() → (string, error)
llm.WithBlockedFunction(typeName, function string) → *LLM

// Env
dag.Env() → *Env
env.WithWorkspace(workspace *Directory) → *Env
env.WithCurrentModule() → *Env          // registers main module's functions as LLM tools
env.WithMainModule(module *Module) → *Env
env.WithSecretInput(name, value, description) → *Env
env.WithStringInput(name, value, description) → *Env
env.Checks(opts) → *CheckGroup           // module-annotated check functions

// Workspace (post-Loop)
workspace.Update() → *Changeset          // .AsPatch(), .Before(), .After(), .DiffStats()
```

## Decision

### D1 — The coder-loop is a function on the .dagger platform module

dagmar's `.dagger/` platform module gains a `Code` function — the coder-loop entry point. The
Phase-0 controller already dispatches `dagger call -m <ref> <fn> <args>` via the agent pod; in
Phase 2, the controller dispatches `dagger call -m <ref> code --source <git-dir> --prompt <md-file> --model <model>`.

```go
// Code is dagmar's coder-loop entry point (Phase 2 cognition).
// It constructs the Env, drives the LLM Loop, and returns the modified workspace Directory.
// The controller dispatches this via `dagger call -m .dagger code ...`.
func (m *Dagmar) Code(
    ctx context.Context,
    // source is the workspace Directory (clone from ADR-0020 D1).
    source *dagger.Directory,
    // promptFile is the resolved prompt .md (ADR-0005 cross-store merge, pre-computed).
    promptFile *dagger.File,
    // model is the LLM model identifier (e.g. "claude-sonnet-4-20250514").
    // +optional
    // +default="anthropic/claude-sonnet-4"
    model string,
    // maxAPICalls bounds the LLM API calls for this Run (token/cost cap).
    // +optional
    // +default=100
    maxAPICalls int,
) (*dagger.Directory, error) {
    return app.Code(ctx, source, promptFile, model, maxAPICalls)
}
```

The function delegates to `app.Code()` — the application-layer binding (ADR-0010 §3: Tier A used
directly in `app/`). This keeps the main-object seam thin and the logic testable.

> **Correction (Review 26 A2):** the Env uses `WithMainModule(projectModule)` to register
> the **project module's** hooks, not `WithCurrentModule()` (which would register
> `.dagger/`'s own functions). See D7 for the corrected hermeticity discussion.

### D2 — Env construction is in the app layer, not the controller

The Env is built **inside the `.dagger` module** (Go code in `app/`), not by the controller in
K8s. The controller's job is to dispatch the function call; the function builds the Env from its
arguments. This is the ADR-0010 §3 pattern: Tier A used directly in `app/`.

```go
// app/code.go (sketch)
func Code(ctx context.Context, source *dagger.Directory, promptFile *dagger.File,
    model string, maxAPICalls int) (string, error) {

    // 1. Build the Env: workspace + project module tools
    // Load the project module (.dagmar/) so its functions become LLM tools
    projectMod := dagger.Connect().ModuleSource(".dagmar").AsModule().Sync(ctx)
    env := dagger.Connect().Env().
        WithWorkspace(source).
        WithMainModule(projectMod)  // registers LLM-Tool hooks (dagmar-issues/memory/prompt)

    // 2. Build the LLM
    llm := dagger.Connect().LLM(dagger.LLMOpts{
        Model:       model,
        MaxAPICalls: maxAPICalls,
    }).
        WithEnv(env).
        WithPromptFile(promptFile)

    // 3. Block on the Loop — the agent works until done or budget exhausted
    llm = llm.Loop()                  // register the loop (lazy)
    llm, err = llm.Sync(ctx)          // force evaluation — blocks until the agent is done
    if err != nil {
        return nil, fmt.Errorf("code: loop failed: %w", err)
    }

    // 4. Token usage (cost accounting — captured here, not by the controller)
    _, _ = llm.TokenUsage().TotalTokens(ctx)  // Phase 2: write to Run output

    // 5. Return the modified workspace
    return source, nil
}
```

**Why `.dagger` module, not controller:** The Env, LLM, and Workspace are all Dagger SDK types —
Tier-A primitives used directly (ADR-0010 §3). The controller (K8s) cannot construct these; it
only orchestrates Runs. The function IS the binding between the K8s control plane (dispatches)
and the Dagger execution plane (builds Env, drives Loop).

### D3 — Multi-step tasks = controller orchestration, not one giant Loop

CONTEXT.md says "A Run drives exactly one Loop." This is preserved. Multi-step tasks are handled
by the **controller's orchestration layer** (ADR-0016 dual-mode Run):

1. An **orchestration Run** references a Workflow (pipeline template, ADR-0016).
2. The controller creates **atomic Sub-Runs**, each driving one Loop.
3. Between Sub-Runs, the controller transitions pipeline state (RED → revise, GREEN → review).

Example: the quality-gate pipeline (ADR-0009 provides the pipeline instance; ADR-0016
provides the orchestration mechanism):
- Sub-Run 1: **coder** — `dagger call -m .dagger code --source <ws> --prompt <coder.md>` → Loop
- Sub-Run 2: **reviewer** — `dagger call -m .dagger code --source <ws> --prompt <review.md>` → Loop (different Agent/Prompt, same module function)

Each Sub-Run's Loop is self-contained: one LLM, one Env, one workspace, one prompt. The
controller sequences them and evaluates outcomes (gate green/red, review approve/veto).

**Why not one Loop for everything:** a single Loop spanning code → review → merge would couple
cognition (code) with judgment (review) and policy (merge) in one uncontrolled LLM context. The
two-green model (ADR-0006) requires separation: the coder and the reviewer are different Agents
with different prompts and tool-sets. Multiple Runs = multiple Loops, each bounded.

### D4 — Token/cost management: MaxAPICalls per Run + TokenUsage tracking

**Budget enforcement:** `LLMOpts{MaxAPICalls: N}` — v0.21.8's built-in API-call cap. When the
LLM hits the cap, the Loop terminates (the agent cannot make more API calls). This is a hard
stop enforced by the engine, not a soft check in dagmar code.

**Budget source:** the `Agent` CRD (Phase 2, not yet implemented) will carry a `maxAPICalls`
field per role (coder: 100, reviewer: 50). The controller passes it through to the `code` function.
A default (100) applies when unset.

**Cost tracking:** after the Loop completes, `llm.TokenUsage()` yields `inputTokens`,
`outputTokens`, `totalTokens`, `cachedTokenReads`, `cachedTokenWrites`. The controller reads
these from the function's return value and writes them to `Run.Status` (or object storage per
ADR-0013 D11). This gives per-Run cost accounting without dagmar implementing its own meter.

**Budget exhaustion = Run failure:** if MaxAPICalls is exhausted before the agent completes, the
Loop ends and the Run's outcome is "incomplete." The controller treats this as a gate failure
(RED) → revise round or human escalation, per the pipeline (ADR-0016).

### D5 — Prompt composition is pre-Loop, in the controller dispatch

ADR-0005's cross-store merge happens **before the `code` function is called**. The controller:

1. Resolves the Agent's prompt refs (project prompt name + dagmar mixin names) from the Agent CRD.
2. Performs the cross-store merge (cn render → JSON → Go section merge → .md file).
3. Passes the resolved `.md` to the `code` function as `promptFile`.

This means the `code` function receives a **pre-composed prompt file** — it does not perform the
merge. The merge is controller-side logic because it needs both canopy stores (dagmar + project),
and the controller has the credential + context to read both. The function's job is to hand the
prompt to the LLM, not to compose it.

**Implementation location:** the merge code lives in the root module (`internal/` or a dedicated
package), called by the controller before dispatching the Run. Not in the `.dagger` module — the
merge is control-plane logic, not execution-plane.

### D6 — Checkable = env.Checks() (from ADR-0020, confirmed)

ADR-0020 D6 established: the in-loop self-verification runs via `env.Checks()` — module-annotated
check functions discovered by the Env. The `code` function's Env construction includes
`WithCurrentModule()`, which registers the project module's functions as tools AND makes its check
functions discoverable via `env.Checks()`.

The LLM can call `env.Checks().Run()` during the Loop to self-verify (ADR-0009: "the gate is the
in-loop self-verification"). The check results appear in the LLM's tool output, and the agent can
iterate if checks fail.

### D7 — Hermeticity: WithMainModule + excluded tools, not network isolation

Per ADR-0011 §2, hermeticity is a **tool-surface constraint**. The `code` function's Env includes
`WithCurrentModule()` (registers the LLM-Tool hooks — dagmar-issues/memory/prompt), but the
default Env tools are curated by the module. Network-capable tools (`http`, `git` remote,
container exec) are NOT on the Env by default — they must be explicitly added.

If a specific Agent role needs a network-capable tool (e.g. a researcher agent that can fetch
URLs), the controller passes a flag, and the `code` function conditionally adds tools. This is
ADR-0011's model: tool-set exclusion is the primary control; the residual network access from raw
container exec is the accepted risk (ProbeNet finding).

For Phase 2 v1, all coder Runs are hermetic: `WithCurrentModule()` only, no network tools. The
agent works on the workspace, calls hooks for issues/memory/prompts, and self-verifies via
`env.Checks()`.

### D8 — Post-Loop: Code returns the modified workspace Directory

After the Loop completes, the workspace has been modified by the agent. The `code` function
returns the **modified workspace as `*dagger.Directory`** — not a string summary. This gives
the controller a concrete handle to the post-Loop state via a second dispatched function.

The PR flow (ADR-0020 D3) is a **separate controller-dispatched function** on the `.dagger`
module:

```go
// Changeset extracts the diff from a pre-Loop workspace and a post-Loop workspace.
func (m *Dagmar) Diff(
    ctx context.Context,
    // before is the pre-Loop workspace (the original clone).
    before *dagger.Directory,
    // after is the post-Loop workspace (Code's return value).
    after *dagger.Directory,
) (*dagger.Changeset, error) {
    return after.Diff(before), nil  // or equivalent v0.21.8 API
}
```

The controller dispatches `Code` first (cognition), then `Diff` (extraction), then
pushes the branch + creates the PR (controller-level, ADR-0020 D3).

**Token usage:** `llm.TokenUsage()` is read inside `Code` and written to the Run's output
(e.g. a sidecar file or struct). The exact mechanism is a Phase-2 implementation detail; the
design principle is that cost accounting is captured inside the function, not by the controller.

## Consequences

- **Two methods on `.dagger` platform module:** `Code(source, promptFile, model, maxAPICalls) → *dagger.Directory`
  and `Changeset(before, after) → *dagger.Changeset`. The Phase-0 dispatch pattern
  (`dagger call -m <ref> <fn>`) extends naturally — both are just functions.
- **Env is execution-plane, not control-plane:** the K8s controller cannot construct Dagger SDK
  types; it dispatches the function, and the function builds the Env. This is ADR-0010 §3.
- **Multi-step = orchestration:** the controller sequences Sub-Runs (ADR-0016). Each Sub-Run
  drives exactly one Loop. No multi-step Loops.
- **MaxAPICalls is the budget:** engine-enforced hard stop. TokenUsage provides accounting.
  Budget exhaustion = Run failure → pipeline revise or escalation.
- **Prompt is pre-composed:** the controller merges canopy stores (ADR-0005) before dispatch.
  The function receives a ready `.md` file.
- **Checkable is env.Checks():** the project module's check functions, discovered natively by
  the Env. No manual gate invocation inside the Loop.
- **Hermetic by default:** `WithCurrentModule()` only; network tools excluded (ADR-0011).
- **Agent CRD needed (Phase 2):** carries model, prompt refs, maxAPICalls, tool-set policy.

## Alternatives considered

- **Loop-wrapping in the controller (K8s-side).** Rejected — the controller cannot construct
  Dagger SDK types (Env, LLM, Workspace). These are execution-plane primitives. The controller
  orchestrates; the module executes.

- **One Loop spanning multiple Agents (coder + reviewer in one context).** Rejected — violates
  ADR-0006 (two-green requires separate Agents with different prompts/tool-sets). Also creates
  an enormous unbounded context — no natural stopping point.

- **Step-based driving (llm.Step() loop in dagmar code).** Considered — `llm.Step()` returns
  after one LLM call, letting dagmar inject logic between steps. Rejected for v1 — adds
  complexity (dagmar manages the step loop, injects checks, handles tool results) for no benefit
  over `llm.Loop()` which handles this internally. `Step()` is available for future
  fine-grained control (e.g. interruptible loops, human-in-the-loop turns).

- **Prompt composition inside the `code` function.** Rejected — the merge needs both canopy
  stores (dagmar + project), and the function only has the project source. The controller has
  both contexts + credentials. Also, prompt composition is control-plane logic (which Agent,
  which mixins), not execution-plane logic.

- **Tool-set as a function parameter (caller chooses tools).** Rejected for v1 — the tool-set
  is determined by the Agent role (hermetic coder vs networked researcher), not by the caller.
  The controller dispatches the same `code` function; the Env's tools come from
  `WithCurrentModule()` (always) plus role-specific additions (future).

- **MaxAPICalls = 0 (unlimited).** Rejected — a Run without a budget is an unbounded cost
  vector. The default (100) is generous for a coding task and can be overridden per Agent.
