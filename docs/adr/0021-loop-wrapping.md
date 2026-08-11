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
// app/code.go (proven — dagmar-9571, empirically validated 2026-08-09)
func Code(ctx context.Context, source *dagger.Directory, promptFile *dagger.File,
    model string, maxAPICalls int, moduleRef string) (*dagger.Directory, error) {

    client := dagger.Connect()
    // 1. Build the Env: Privileged + Writable + directory I/O bindings.
    //    Privileged=true grants the LLM core API access (Directory, File, Container) —
    //    without it, the agent sees only DeclareOutput + ReadLogs (no file tools).
    //    Writable=true allows the agent to Save outputs.
    //    See ADR-0022 for the full rationale + hermeticity analysis.
    env := client.Env(dagger.EnvOpts{
        Privileged: true,
        Writable:   true,
    }).
        WithDirectoryInput("source", source, "The project source directory").
        WithDirectoryOutput("result", "The modified source directory")

    // 2. Optionally register the project module's LLM-Tool hooks (ADR-0019)
    if moduleRef != "" {
        projectMod := client.ModuleSource(moduleRef).AsModule().Sync(ctx)
        env = env.WithMainModule(projectMod)
    }

    // 3. Build the LLM
    llm := client.LLM(dagger.LLMOpts{
        Model:       model,
        MaxAPICalls: maxAPICalls,
    }).
        WithEnv(env).
        WithPromptFile(promptFile)

    // 4. Block on the Loop — the agent works until done or budget exhausted
    llm = llm.Loop()
    if _, err := llm.Sync(ctx); err != nil {
        return nil, fmt.Errorf("code: loop failed: %w", err)
    }

    // 5. Token usage (cost accounting — captured here, not by the controller)
    _, _ = llm.TokenUsage().TotalTokens(ctx)

    // 6. Return the modified workspace from the "result" output binding
    return env.Output("result").AsDirectory(), nil
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
2. ~~The controller creates **atomic Sub-Runs**, each driving one Loop.~~
   → **Revised (ADR-0027):** the controller dispatches a single CognitionRun pipeline call.
   The pipeline chains Prompt→Code→Gate→Review→Adjudicate as Go-method calls within the Dagger
   module. Each LLM role still drives exactly one Loop, but the chaining is Dagger-native
   (custom object state serialization), not controller-orchestrated Sub-Runs.
3. ~~Between Sub-Runs, the controller transitions pipeline state (RED → revise, GREEN → review).~~
   → **Revised:** the pipeline handles revise loops internally (gate-red → re-code in the same
   LLM context). The controller evaluates the final pipeline result (approve/escalate).

Example (revised, ADR-0027): the quality-gate pipeline is a single CognitionRun call:
- `dagger call cognition-run --source <ws> --task-context <text> --model <m>`
- Internally: Prompt → Code (Loop) → Gate (Container.WithExec) → Review (Loop) → Adjudicate (Loop)
- Revise loops keep the coder's LLM context (the agent remembers prior failed attempts)
- Returns structured JSON: {outcome, gate_result, review_verdict, coverage_bps, rounds}

Each role's Loop is still self-contained: one LLM, one Env, one workspace, one prompt.
The two-green model (ADR-0006) is preserved — coder and reviewer are different Agents
with separate Loops, just chained within one Dagger function call instead of separate pods.

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
`WithMainModule(projectModule)`, which registers the project module's functions as tools AND makes its check
functions discoverable via `env.Checks()`.

The LLM can call `env.Checks().Run()` during the Loop to self-verify (ADR-0009: "the gate is the
in-loop self-verification"). The check results appear in the LLM's tool output, and the agent can
iterate if checks fail.

### D7 — Hermeticity: Privileged Env + tool-set policy (revised post-cognition-proof)

> **Revised 2026-08-09 (Review 29 D1/E2):** the cognition proof (dagmar-9571) empirically showed
> that `EnvOpts{Privileged: true}` is **required** for the LLM to access file tools
> (Directory, File, Container). Without it, the agent sees only `DeclareOutput` + `ReadLogs` —
> no file read/write. `Privileged` grants "core API including host access" (v0.21.8 SDK doc),
> which is broader than ADR-0011's tool-exclusion model anticipated. ADR-0022 reconciles this
> tension; the interim position is documented here.

The Env is constructed with `Privileged: true` (the agent gets core Dagger API access). The
hermeticity constraint (ADR-0011) is enforced via **tool-set policy**, not via the Env constructor:
the project module's functions are registered via `WithMainModule(projectModule)` (the LLM-Tool
hooks — dagmar-issues/memory/prompt). Network-capable tools (`http`, `git` remote, container exec)
are NOT added to the Env by default.

The residual risk is that `Privileged` grants **host access** — a capability ADR-0011's
tool-exclusion model does not cover. This is the accepted interim risk for Phase 2 v1, documented
in ADR-0022. The agent works on the workspace directory (input/output bindings, not host mounts),
calls hooks for issues/memory/prompts, and the Loop runs inside the Dagger engine sandbox.

For Phase 2 v1, all coder Runs use the Privileged Env without network tools. `ToolSetPolicy`
(`agent_types.go`) is defined but NOT YET READ by the controller — it will control whether
network-capable tools are added when networked agent roles are needed (Phase 3).

### D8 — Post-Loop: Code returns the modified workspace Directory

After the Loop completes, the workspace has been modified by the agent. The `code` function
returns the **modified workspace as `*dagger.Directory`** — not a string summary. This gives
the controller a concrete handle to the post-Loop state via a second dispatched function.

The PR flow (ADR-0020 D3) is a **separate controller-dispatched function** on the `.dagger`
module:

```go
// Diff extracts the changes from a post-Loop workspace vs. the pre-Loop baseline.
func (m *Dagmar) Diff(
    ctx context.Context,
    // after is the post-Loop workspace (Code's return value).
    after *dagger.Directory,
    // before is the pre-Loop workspace (the original clone).
    before *dagger.Directory,
) *dagger.Directory {
	return app.Diff(after, before)
}
```

The controller dispatches `Code` first (cognition), then `Diff` (extraction), then
pushes the branch + creates the PR (controller-level, ADR-0020 D3).

**Token usage:** `llm.TokenUsage()` is read inside `Code` and written to the Run's output
(e.g. a sidecar file or struct). The exact mechanism is a Phase-2 implementation detail; the
design principle is that cost accounting is captured inside the function, not by the controller.

## Consequences

- **Two methods on `.dagger` platform module:** `Code(source, promptFile, model, maxAPICalls, moduleRef) → (*dagger.Directory, error)`
  and `Diff(after, before) → *dagger.Directory`. The Phase-0 dispatch pattern
  (`dagger call -m <ref> <fn>`) extends naturally — both are just functions.
- **Env is execution-plane, not control-plane:** the K8s controller cannot construct Dagger SDK
  types; it dispatches the function, and the function builds the Env. This is ADR-0010 §3.
- **Multi-step = pipeline chaining (ADR-0027):** the CognitionRun pipeline chains multiple
  Loops (one per LLM role) as Go-method calls within the Dagger module. Each role drives
  exactly one Loop. ~~The controller sequences Sub-Runs (ADR-0016).~~ No multi-step Loops.
- **MaxAPICalls is the budget:** engine-enforced hard stop. TokenUsage provides accounting.
  Budget exhaustion = Run failure → pipeline revise or escalation.
- **Prompt is pre-composed:** the controller merges canopy stores (ADR-0005) before dispatch.
  The function receives a ready `.md` file.
- **Checkable is env.Checks():** the project module's check functions, discovered natively by
  the Env. No manual gate invocation inside the Loop.
- **Privileged Env + tool-set policy:** `EnvOpts{Privileged: true}` is required for file access
  (ADR-0022); hermeticity enforced via tool-set exclusion, not Env constructor (ADR-0011, revised D7).
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
  `WithMainModule(projectModule)` (always) plus role-specific additions (future).

- **MaxAPICalls = 0 (unlimited).** Rejected — a Run without a budget is an unbounded cost
  vector. The default (100) is generous for a coding task and can be overridden per Agent.
