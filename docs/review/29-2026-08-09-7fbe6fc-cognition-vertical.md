# Review 29 — Cognition Vertical (code() Env fix, prompt composition, controller wiring)

- **Scope:** `5e16a27..7fbe6fc` — cognition-vertical: `code()` Env construction rewritten to the proven Privileged/Writable/DirectoryInput/Output pattern; ADR-0005 prompt composition (`internal/prompt/compose.go`); controller cognition-Run wiring (workspace clone + prompt dispatch + `code()` args); `maxReviseRoundsFor` wired in (Review 28 A1/A2 fixes); ADR-0021 D6/D7/D8 corrections.
- **Baseline:** `7fbe6fc`
- **Changed files (11):** `code.go` (Env rewrite), `orchestration.go` (maxReviseRoundsFor wired), `run_controller.go` (cognition-Run: clone + prompt + args), `run_controller_test.go` (cognition test), `compose.go` + `compose_test.go` (new prompt package), `0021-loop-wrapping.md` (D6/D7/D8 corrections), `SKILL.md` (_rlm param), mulch + seeds sync.
- **Build gate:** `go build ./...` + `.dagger/...` green; `go vet` green; `go test ./internal/...` 6 tests pass.
- **Tag legend:** `[FIX]` contradiction/standards breach, fix now · `[GAP]` referenced but undecided, needs ADR/glossary · `[HOUSE]` doc structure · `[SPEC]` deviation from the seed/ADR ask

---

## A. Standards

The range resolves Review 28's two key findings cleanly: `maxReviseRoundsFor()` is now wired into both `advanceCoder` (L100) and `advanceReview` (L137) — the hardcoded `maxRound := 3` is gone; and `code.go` now uses a single `client := dagger.Connect()` (Review 28 A2's triple-Connect regression is fixed). The `--max-apicalls` CLI flag change is **correct** — verified via `dagger call code --help`: the actual v0.21.8 flag is `--max-apicalls`, not the prior `--max-api-calls`. The cognition test (`TestReconcile_CognitionRunInjectsWorkspaceAndPrompt`) correctly validates the generated command shape.

However, the code() Env rewrite introduces the range's most significant issue: a code-doc divergence between the proven implementation and every design document that describes it.

### A1 — `[FIX]` Env construction diverges from ALL design docs

**`code.go` L52–57, L88 vs CONTEXT.md L41–49, ADR-0021 D2 sketch, ADR-0020 D1–D2:**

The code builds the Env with an entirely different API than every document describes:

| Aspect | Docs (CONTEXT.md, ADR-0021, ADR-0020) | Code (`code.go`) |
|--------|---------------------------------------|------------------|
| Env construction | `env.WithWorkspace(source)` | `Env(EnvOpts{Privileged: true, Writable: true})` |
| Source binding | `WithWorkspace(source)` | `WithDirectoryInput("source", source, ...)` |
| Result extraction | `workspace.Update() → *Changeset` | `env.Output("result").AsDirectory()` |
| Output declaration | (implicit via Workspace) | `WithDirectoryOutput("result", ...)` |

The terms `EnvOpts`, `Privileged`, `Writable`, `WithDirectoryInput`, `WithDirectoryOutput`, and `env.Output().AsDirectory()` appear **nowhere** in CONTEXT.md glossary, the ADR-0021 v0.21.8 API surface listing (§Context), or any ADR. The commit message says "cognition vertical PROVEN" — the implementation is empirically correct, but the design docs describe a superseded pattern. Fix: update CONTEXT.md Env/Workspace/Changeset entries + ADR-0021 D2 sketch + ADR-0020 D1 to match the proven pattern (or add an ADR for the shift — see E1).

### A2 — `[FIX]` Prompt composition non-functional at runtime

**`compose.go` L97–106, `run_controller.go` L342, L359:**

Two compounding defects make the ADR-0005 prompt composition silently fail:

1. **`ShellComposeCommand` discards `workspaceDir`** (L102: `_ = workspaceDir`). The generated `cn render <prompt>` runs from the pod's default cwd, not `/workspace/.canopy/`. The function that *does* `cd` correctly (`ShellRenderCommand`, L84–88) is never called.

2. **`cn` is not installed in the pod.** `agentPodFor` runs `apk add kubectl curl` (or `kubectl curl git`). Canopy CLI (`cn`, bun-installed — see `docs/research/canopy-prompt-model.md` L4) is never provisioned. So `cn render` fails with "command not found" → `2>/dev/null` swallows it → the `echo` fallback always fires.

3. **The fallback `echo` emits literal `\n`** (not newlines). The Go raw-string `\\n` produces `\n` in the shell command; busybox `echo` without `-e` prints literal backslash-n. The stub prompt is a single garbled line.

Net effect: every cognition Run receives `# Agent Prompt\n\nProject prompt: <name>` (literal) instead of the resolved canopy prompt. The feature is wired but produces only a broken stub.

### A3 — `[GAP]` `Compose()` + `ShellRenderCommand()` are dead code; `DagmarMixins` never read

**`compose.go` L25, L84; `agent_types.go` L31–33:**

- `Compose()` — implements ADR-0005 Variant A's Go-side section merge (the load-bearing piece). Has 4 unit tests. Never called by any production code path.
- `ShellRenderCommand()` — correctly `cd`s into the canopy dir before rendering. Never called.
- `PromptRef.DagmarMixins` — carries dagmar operational mixin names. The controller reads only `agent.Spec.Prompt.ProjectPrompt` (`run_controller.go` L169); `DagmarMixins` is never consumed.

Only `ShellComposeCommand()` (the MVP-only stub) is used. The ADR-0005 3-step process (render both stores → Go merge → write `.md`) is structurally unimplemented: `Compose()` expects `[]Section` (JSON-parsed), but the shell path produces raw text. These should be clearly marked as deferred or removed to avoid implying the feature works.

---

## B. Spec

### B1 — `[SPEC]` Seed dagmar-bb43 (closed) claims ADR-0005 "implemented"; full cross-store merge is not

**Seed dagmar-bb43 resolution:** "Implemented. internal/prompt/compose.go with Compose() + ShellComposeCommand(). 18 tests, gate green."

The seed is closed as done, but the deliverable is partial:
- Only the project prompt is rendered (no dagmar mixin merge — `DagmarMixins` unread, A3).
- `Compose()` is dead code (A3).
- The shell path doesn't work at runtime (A2).
- 18 tests pass in isolation but don't cover the end-to-end flow (no test asserts `cn` is installed or that `ShellComposeCommand` enters the workspace).

ADR-0005 Variant A step 3 (the Go merge) exists as code but is never exercised. The seed should be reopened or a follow-up seed filed for the runtime gap.

### B2 — `[HOUSE]` ADR-0021 Consequences still lacks `moduleRef` in Code signature

**ADR-0021 L247:** `Code(source, promptFile, model, maxAPICalls) → *dagger.Directory` — the actual signature is `Code(source, promptFile, model, maxAPICalls, moduleRef) → (*dagger.Directory, error)`. The `moduleRef` param and the `error` return were added to the code but not to the Consequences summary.

### B3 — `[HOUSE]` ADR-0021 D8 Diff code block has a stray `}`

**ADR-0021 L236–237:** the Diff code block has an extra closing brace after the function:

```go
) *dagger.Directory {
	return app.Diff(after, before)
}
}   ← stray
```

---

## C. Inconsistencies / contradictions

### C1 — `[FIX]` CONTEXT.md Env/Workspace/Changeset entries describe the superseded `WithWorkspace` API

Same root as A1, focused on CONTEXT.md:
- **L42:** `env.WithWorkspace(source).WithMainModule(projectModule)` — code uses `WithDirectoryInput`.
- **L46–47:** Workspace "passed to the agent's Env via `env.WithWorkspace(source)`" — code uses `WithDirectoryInput("source", ...)`.
- **L49:** "Post-Loop changes captured via `workspace.Update()` → `*Changeset`" — code reads `env.Output("result").AsDirectory()`.
- **L65–67:** Changeset entry references `Workspace.Update()` path — code no longer uses it.

These are the glossary's authoritative definitions; they now mislead any reader (human or agent) trying to understand the Env construction.

### C2 — `[HOUSE]` ADR-0021 D2 sketch still uses `WithWorkspace`

**ADR-0021 D2 sketch (L95–110):** the sketch was partially updated (added `moduleRef` param, single `client`) but still shows `env := client.Env().WithWorkspace(source).WithMainModule(projectMod)`. The actual code uses `Env(EnvOpts{...}).WithDirectoryInput(...).WithDirectoryOutput(...)`.

### C3 — `[HOUSE]` Diff parameter order disagrees: main.go vs ADR-0021 D8

**`main.go` L128–136:** `Diff(ctx, after, before)` — after is the first Directory param.
**ADR-0021 D8 sketch:** `Diff(ctx, before, after)` — before is the first Directory param.

Both internally call `app.Diff(after, before)`, so the app layer is consistent. But the public main-module parameter order disagrees with the ADR. Pick one and align both.

---

## D. Undefined / under-defined terms

### D1 — `[GAP]` `Privileged: true` contradicts the hermeticity model (ADR-0011, ADR-0021 D7)

**`code.go` L52–53:** `EnvOpts{Privileged: true, ...}`.

The v0.21.8 SDK documents `Privileged` as: *"Give the environment the same privileges as the caller: core API including **host access**, current module, and dependencies."*

ADR-0021 D7 states: *"For Phase 2 v1, all coder Runs are hermetic: `WithMainModule(projectModule)` only, no network tools."*

ADR-0011 §2 establishes hermeticity as a tool-surface constraint, but `Privileged: true` grants host-level access — a category of capability ADR-0011's tool-exclusion model doesn't address. The code comment (L18–20) explains Privileged is needed so the agent "can read/write files" (without it, only DeclareOutput + ReadLogs are available). This is a genuine tension: the agent needs Privileged to work on files, but Privileged grants host access that breaks the hermeticity claim. No ADR reconciles this. → E1/E2.

---

## E. Referenced-but-missing ADRs

### E1 — `[GAP]` No ADR for the Env construction pattern shift (WithWorkspace → Privileged/Writable/IO bindings)

The move from `env.WithWorkspace(source)` to `Env(EnvOpts{Privileged, Writable}).WithDirectoryInput().WithDirectoryOutput()` is a significant design decision driven by empirical discovery (the documented pattern didn't produce a working Loop). This needs an ADR documenting: (a) why WithWorkspace didn't work, (b) the proven pattern, (c) the Privileged/hermeticity tension (D1). Priority: **highest** — this is the load-bearing cognition pattern and it's undocumented.

### E2 — `[GAP]` No ADR reconciling Privileged:true with hermeticity

ADR-0011 and ADR-0021 D7 both claim hermeticity via tool-set exclusion. `Privileged: true` grants host access, which is not covered by tool-set exclusion. Either: (a) update the hermeticity model to scope what Privileged actually grants vs what ADR-0011's ProbeNet finding established, or (b) document the accepted residual risk. Priority: **high** — the security model has an unstated hole.

---

## F. Housekeeping

### F1 — `[HOUSE]` `min()` in test shadows Go 1.21+ builtin

**`run_controller_test.go` L492–496:** defines `func min(a, b int) int`. Go 1.26.1 (per `go.mod`) has a builtin `min` since 1.21. The custom definition is unnecessary dead code that shadows the builtin. Remove it; the call site (L490: `s[:min(len(s), 200)]`) works with the builtin.

### F2 — `[GAP]` `ToolSetPolicy` still inert (carried from Review 27 A3)

**`agent_types.go` L7–18:** `ToolSetPolicy` (`hermetic`/`networked`) is defined and documented but no code path reads it. This was noted in Review 27 A3 and remains unaddressed. With `Privileged: true` now in the code (D1), the relationship between `ToolSetPolicy` and the Env construction is even more unclear — the field implies tool-set control exists, but the Env grants host access unconditionally.

---

## Suggested next ADRs (priority order)

1. **ADR-0022: Env construction for the LLM Loop** — document the proven Privileged/Writable/DirectoryInput/DirectoryOutput pattern; explain why `WithWorkspace` didn't work; reconcile `Privileged: true` with the hermeticity model (ADR-0011). This is the single most important documentation gap: the cognition vertical's core mechanism is undocumented.
2. **ADR-0023 (or amend ADR-0005): Prompt composition runtime delivery** — how `cn`/canopy is provisioned in the agent pod; how `DagmarMixins` are rendered and merged; the controller→pod→canopy→prompt-file chain. The current implementation is structurally incomplete.
3. **Amend ADR-0021:** sweep D2 sketch (Env construction), D8 (stray `}`, parameter order), and Consequences (add `moduleRef` + `error` to the signature) to match the code.

---

## Already tracked in seeds

| Seed | Status | Coverage |
|------|--------|----------|
| dagmar-bb43 | closed | ADR-0005 prompt composition — closed as "implemented" but B1/A2/A3 show it's partial and non-functional at runtime. Needs reopening or a follow-up. |
| dagmar-9571 | closed | Prove cognition vertical — closed as "proven". The Loop mechanism works; the prompt delivery path (A2) does not. |
| dagmar-8d4a | open | Loop-wrapping ADR (ADR-0021) — ADR exists but D2 sketch + Consequences are now stale (C2, B2). |

No open seeds cover the Env construction divergence (A1/C1), the Privileged/hermeticity tension (D1/E2), or the prompt runtime gap (A2).

## Newly surfaced (observations — NOT filed as seeds)

- **A1/C1** — Env construction code-doc divergence (all design docs stale).
- **A2** — Prompt composition non-functional at runtime (`cn` not installed; workspaceDir discarded).
- **A3** — Dead code (`Compose()`, `ShellRenderCommand()`; `DagmarMixins` unread).
- **D1/E1/E2** — `Privileged: true` vs hermeticity tension; no ADR.
- **B2/B3/C2/C3** — ADR-0021 partial-update sweep items.
- **F1** — `min()` builtin shadow.
- **F2** — `ToolSetPolicy` still inert (carried forward).
