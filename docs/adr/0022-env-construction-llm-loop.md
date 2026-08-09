# ADR-0022: Env construction for the LLM Loop — Privileged/Writable/IO bindings

Date: 2026-08-09
Seed: dagmar-9571 (cognition vertical proof) · Status: **ACCEPTED**

## Context

ADR-0021 specified the Loop-wrapping design (`code()` function building an Env, driving
`dag.LLM().Loop()`). The D2 sketch described the Env as `client.Env().WithWorkspace(source)`.
The cognition vertical proof (dagmar-9571, empirically validated 2026-08-09) discovered that this
construction **does not produce a working Loop** — the LLM cannot read or write files.

Three issues were identified:

1. **No file tools without Privileged.** `Env()` without `Privileged: true` gives the LLM only
   `DeclareOutput` + `ReadLogs`. It cannot call `Directory.file()`, `Directory.withNewFile()`,
   `File.contents()`, or any core Dagger API. The agent is cognitively blind — it can see the
   workspace exists but cannot interact with it.

2. **WithWorkspace alone does not provide write access.** `Env.WithWorkspace(dir)` registers the
   workspace as read-only context. The LLM can list objects but cannot save modifications. The
   proven pattern uses `WithDirectoryInput("source", dir)` + `WithDirectoryOutput("result")`:
   the agent reads from "source", modifies via `Directory.withNewFile`, and saves the result
   via the `Save` tool to "result". Post-Loop, `env.Output("result").AsDirectory()` retrieves
   the modified workspace.

3. **CLI flag mismatch.** Dagger v0.21.8 kebab-cases `maxAPICalls` as `--max-apicalls`, not
   `--max-api-calls` (the controller had the wrong form).

## Decision

### The Env is constructed with Privileged + Writable + directory I/O bindings

```go
env := client.Env(dagger.EnvOpts{
    Privileged: true,
    Writable:   true,
}).
    WithDirectoryInput("source", source, "The project source directory").
    WithDirectoryOutput("result", "The modified source directory")
```

- **`Privileged: true`** — mandatory: grants the LLM access to the core Dagger API (Directory,
  File, Container). Without it, no file tools are available.
- **`Writable: true`** — mandatory: allows the LLM to declare and `Save` outputs.
- **`WithDirectoryInput("source", dir)`** — binds the project source as a readable directory.
- **`WithDirectoryOutput("result")`** — declares an output slot the agent saves to via `Save`.

After the Loop: `env.Output("result").AsDirectory()` returns the agent's modified workspace.

### Hermeticity reconciliation (D1/E2 from Review 29)

`Privileged: true` grants "core API including host access" (v0.21.8 SDK). ADR-0011/0021 D7
originally claimed hermeticity via tool-set exclusion alone. The tension: the agent needs
Privileged to work on files, but Privileged grants broader access than ADR-0011 anticipated.

**Interim position (Phase 2 v1):** the accepted residual risk is that `Privileged` grants host
access at the SDK level. In practice, the agent operates on directory input/output bindings
(in-memory DAG objects), not host mounts. The Loop runs inside the Dagger engine sandbox.
Network-capable tools (`http`, `git` remote) are NOT added to the Env. The project module's
functions (`WithMainModule`) are the only additional tools. This is the same category of accepted
residual as ADR-0011 §3's "raw exec path" — a capability boundary that is consciously accepted.

**Future hardening (Phase 3+):** investigate whether `Privileged` can be scoped (e.g. via a
custom tool-set that excludes host-access functions), or whether a future Dagger version provides
a file-tools-only mode without full Privileged. Until then, `ToolSetPolicy` (agent_types.go)
remains defined but inert — it will control network tool addition when needed.

## Consequences

- **ADR-0021 D2/D7/D8 revised** to match this pattern (this ADR supersedes the original sketches).
- **CONTEXT.md glossary updated** — Env, Workspace, Changeset entries now describe the proven
  `Privileged`/`Writable`/IO-binding pattern.
- **`code.go` returns `env.Output("result").AsDirectory()`**, not the original `source` directory.
  The original source is the pre-Loop baseline; the "result" output is the post-Loop modified copy.
- **The `--max-apicalls` flag** is the correct v0.21.8 CLI form (controller fixed).
- **Prompt composition (ADR-0005)** remains structurally present but not runtime-functional
  (canopy CLI not yet provisioned in the agent pod). A follow-up is needed for runtime delivery.
