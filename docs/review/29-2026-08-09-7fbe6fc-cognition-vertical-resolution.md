# Review 29 Resolution — Cognition Vertical

Date: 2026-08-09
Review: `docs/review/29-2026-08-09-7fbe6fc-cognition-vertical.md`
Baseline: `7fbe6fc`

## Findings addressed

### [FIX] A1/C1 — Env construction diverges from ALL design docs
**Status: FIXED.** Updated CONTEXT.md (Env, Workspace, Changeset entries) + ADR-0021 D2 sketch
to describe the proven `EnvOpts{Privileged, Writable}` + `WithDirectoryInput/Output` pattern.
ADR-0022 written as the canonical record.

### [FIX] A2 — Prompt composition non-functional at runtime
**Status: FIXED (stub) + DEFERRED (full delivery).** `ShellComposeCommand` now:
- Uses `workspaceDir` (no longer discarded)
- Falls back to `printf` (real newlines, not literal `\n` via `echo`)
- `cn` provisioning deferred to dagmar-3302

### [GAP] A3 — Dead code (Compose(), ShellRenderCommand(), DagmarMixins)
**Status: DOCUMENTED.** Package doc + function comments now clearly mark Compose() and
ShellRenderCommand() as structurally present but not wired. DagmarMixins remains in the type
definition for the future runtime delivery (dagmar-3302).

### [GAP] D1/E1/E2 — Privileged:true vs hermeticity
**Status: ADR-0022 WRITTEN.** Documents the tension, the interim accepted residual risk, and
the future hardening direction.

### [HOUSE] B2 — ADR-0021 Consequences missing moduleRef + error
**Status: FIXED.** Signature updated to `Code(source, promptFile, model, maxAPICalls, moduleRef) → (*dagger.Directory, error)`.

### [HOUSE] B3 — ADR-0021 D8 stray `}`
**Status: FIXED.** Removed extra closing brace.

### [HOUSE] C2 — ADR-0021 D2 sketch still uses WithWorkspace
**Status: FIXED.** D2 sketch replaced with the proven pattern (ADR-0022 reference).

### [HOUSE] C3 — Diff parameter order
**Status: FIXED.** ADR-0021 D8 sketch aligned to code: `Diff(after, before)` (after first).

### [HOUSE] F1 — min() shadows Go builtin
**Status: FIXED.** Removed dead `func min(a, b int) int`.

### [SPEC] B1 — Seed dagmar-bb43 closed prematurely
**Status: FOLLOW-UP FILED.** dagmar-3302 covers the runtime delivery gap.

### [GAP] F2 — ToolSetPolicy still inert
**Status: DOCUMENTED in ADR-0022.** Explicitly noted as defined-but-inert; will control
network tool addition in Phase 3.

## Files changed

- `internal/prompt/compose.go` — package doc + ShellComposeCommand fix (A2, A3)
- `internal/controller/run_controller_test.go` — removed min() shadow (F1)
- `CONTEXT.md` — Env/Workspace/Changeset entries updated (A1, C1)
- `docs/adr/0021-loop-wrapping.md` — D2 sketch, D7 hermeticity, D8 stray }, Consequences (B2, B3, C2, C3, D7)
- `docs/adr/0022-env-construction-llm-loop.md` — new ADR (D1, E1, E2)
- `docs/review/29-2026-08-09-7fbe6fc-cognition-vertical-resolution.md` — this file

## Seeds filed

- dagmar-3302 — Prompt composition runtime delivery (A2 full, A3 wiring)
