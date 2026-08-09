# Review 27 — Phase 2 Implementation (CRD types + controller orchestration + code()/diff())

- **Scope:** `5d52cc3..7a78c79` — Phase 2 vertical: Agent/Prompt/QualityGate/Workflow CRD types (`api/v1alpha1/`), Run dual-mode extension + AgentRef binding, controller orchestration state machine (`orchestration.go` + tests), `code()`/`Diff()` functions on `.dagger` module + `app.Code`, ADR-0016–0021 doc corrections, dagmar-review skill consolidation.
- **Baseline:** `7a78c79`
- **Changed files (37):** 5 new CRD type files + deepcopy, Run types extension, orchestration.go + orchestration_test.go (446 lines), run_controller.go dual-mode + AgentRef wiring, `.dagger/main.go` Code+Diff, `.dagger/internal/app/code.go`, `dagger.gen.go` regenerated, 5 new CRD YAML manifests, CONTEXT.md revision, ADR-0005/0013/0017/0018/0019/0020/0021 corrections, dagmar-review skill rewrite.
- **Tag legend:** `[FIX]` contradiction/standards breach, fix now · `[GAP]` referenced but undecided, needs ADR/glossary · `[HOUSE]` doc structure · `[SPEC]` deviation from the seed/ADR ask

---

## A. Standards

The Phase 2 vertical is architecturally sound: CRD types are well-commented and follow kubebuilder conventions; the orchestration state machine is cleanly factored (`reconcileOrchestration` → `advanceCoder`/`advanceReview` → `getOrCreateSubRun`/`transitionPipeline`); the dual-mode Run validation is symmetric and fail-fast; tests cover the four key pipeline transitions (create → review → done → escalate). The `code()` function correctly uses `WithMainModule(projectMod)` (Review 26 A2 fix) and `Loop().Sync(ctx)` (lazy register + force evaluation). ADR-0021's design intent is faithfully realized.

**Build gate:** `go build`, `go vet`, `go test` all green for `./...` and `.dagger/...`. 10 tests pass.

Fowler smells:

- **Duplicated Code** — `maxRound := 3` hardcoded identically in `advanceCoder` (L100) and `advanceReview` (L138). The value exists on `QualityGateSpec.MaxReviseRounds` (default 3) and the Workflow references a `QualityGateRef`, but the orchestration never reads it. → A1
- **Mysterious Name** — `shellJoin` has a garbled doubled doc comment (L480–483): the first line is a leftover from the pre-dual-mode version, the second overwrites it mid-sentence. → A2
- **Speculative Generality** — `ToolSetPolicy` (hermetic/networked) is defined on `AgentSpec` with doc-block validation but never read by the controller or passed to the `code()` function. The `code()` Env always registers only the project module tools. → A3

### A1 — `[FIX]` Hardcoded `maxRound := 3` duplicated, ignores QualityGate.MaxReviseRounds

**`orchestration.go` L100, L138:** both `advanceCoder` and `advanceReview` hardcode `maxRound := 3` with the comment `// default; could read from QualityGate spec`. The Workflow carries `QualityGateRef`, and `QualityGateSpec.MaxReviseRounds` (default 3) is the designated home for this value. The orchestration should fetch the QualityGate and read `MaxReviseRounds`. This is both a DRY violation and a spec-implementation gap: the value is configurable in the CRD but dead in the controller.

### A2 — `[FIX]` Doubled/garbled `shellJoin` doc comment

**`run_controller.go` L480–483:** two comment lines start with "shellJoin joins…" — the first is stale ("Phase 0 spike; no escaping — ModuleArgs are"), the second is current. The result reads as a broken sentence. Delete the stale first line.

### A3 — `[GAP]` `ToolSetPolicy` defined but never wired

**`agent_types.go` L7–18, L61–65:** `ToolSetPolicy` and its two constants (`hermetic`, `networked`) are defined and documented, but no code path reads `Agent.Spec.ToolSetPolicy`. The controller extracts only `Model` and `MaxAPICalls` from the Agent spec (`run_controller.go` L162–163). The `code()` function doesn't accept a tool-set parameter. This is likely intentional for Phase 2 incremental scope, but the field appears load-bearing in its doc comments ("ADR-0011") while being inert.

---

## B. Spec

### B1 — `[SPEC]` WorkflowSpec diverges from ADR-0016 (agents map → typed fields, MaxReviseRounds dropped)

**ADR-0016 §1** specifies:

| Field | Type |
|---|---|
| `agents` | `map[string]string` (role → Agent name) |
| `maxReviseRounds` | `int` (on WorkflowSpec) |
| `qualityGateRef` | `string` |
| `requiresTwoGreen` | `bool` |

**Implementation** (`workflow_types.go`): `agents` map replaced by explicit `CoderAgentRef` + `ReviewerAgentRef` fields; `maxReviseRounds` absent from WorkflowSpec (moved to `QualityGateSpec.MaxReviseRounds`). The typed-field approach is arguably better (schema validation, clearer for the quality-gate family), but **ADR-0016 was not updated** to reflect this change. ADR-0016 §3 and §4 still reference `agents` and `maxReviseRounds` as WorkflowSpec fields.

### B2 — `[SPEC]` Function named `Diff()`, ADR-0021 + seed name it `Changeset()`

**ADR-0021 D8** (L224–247) + **seed dagmar-6594** title: "Implement code() + **Changeset()** functions." The implementation names it `Diff()` (`.dagger/main.go` L123, `app.Diff`). The seed resolution acknowledges `app.Diff()` but the ADR + CONTEXT.md + Consequences section consistently say `Changeset`. This is a naming drift that should be reconciled — either rename to match the ADR or update the ADR.

### B3 — `[SPEC]` Orchestration Sub-Runs cannot dispatch `code()` — missing `--source` and `--prompt-file`

**`orchestration.go` L176–177:** Sub-Runs are created with `ModuleFunction: "code"` and no `ModuleArgs` for `--source` or `--prompt-file`. The `Code()` function in `.dagger/main.go` has **no `+optional` tag** on `source` or `promptFile` — they are required parameters. When the controller dispatches a Sub-Run's agent pod, the command becomes:

```
dagger call -m <ref> code --model X --max-api-calls Y
```

Both `--source` (workspace clone, ADR-0020 D1) and `--prompt-file` (composed prompt, ADR-0005) are absent. The orchestration state machine is correct and tested, but the atomic Sub-Run dispatch is incomplete: workspace cloning and prompt composition are not wired. The `reconcileOrchestration` comment (L30–33) says "gate-green is assumed when the coder Sub-Run succeeds" — but the gap is wider than gate evaluation. It encompasses the entire workspace + prompt dispatch path. This should be tracked as a known Phase-2 gap, not just "gate evaluation deferred."

---

## C. Inconsistencies / contradictions

### C1 — `[FIX]` ADR-0021 D7 still says `WithCurrentModule()` throughout (6+ stale references)

**ADR-0021 L199–215 (D7) + L259 + L286 (Consequences):** D7 is titled "Hermeticity: `WithCurrentModule` + excluded tools" and body text says `WithCurrentModule()` 6+ times. The Review 26 A2 correction at L80 patches D2 to say `WithMainModule(projectModule)`, but D7 + Consequences were left stale. These sections now contradict the code (`code.go` L48: `WithMainModule(projectMod)`) and the D2 correction.

### C2 — `[FIX]` ADR-0021 D2 code sketch returns `(string, error)`, contradicting D1/D8/implementation

**ADR-0021 L91:** the D2 "sketch" shows `func Code(...) (string, error)`, but D1 (L71), D8, and the actual implementation all return `(*dagger.Directory, error)`. The sketch should be updated to match.

### C3 — `[FIX]` PipelinePhase docstring lists `"gate"` — no `PipelineGate` constant, phase never entered

**`run_types.go` L85–86:** docstring says `Values: "gate" | "coder" | "review" | "escalated" | "done"`. ADR-0016 also lists `"calibration"` (deferred). The orchestration constants (`orchestration.go` L17–20) define only `PipelineCoder`, `PipelineReview`, `PipelineDone`, `PipelineEscalated`. No `PipelineGate` constant exists, and the "gate" phase is never entered (gate evaluation deferred per L30–33). The docstring should either drop `"gate"` or add a note that it's deferred.

### C4 — `[FIX]` CONTEXT.md Changeset glossary entry wrong about return type

**CONTEXT.md L65–66:** says `after.Diff(before) → *Changeset`. In v0.21.8, `Directory.Diff()` returns `*Directory` (verified in `dagger.gen.go` L4350), not `*Changeset`. The `Changeset` type exists separately (`Changeset.AsPatch()`, `.Before()`, `.After()`, `.DiffStats()` are on `*Changeset`, not on the Diff result). CONTEXT.md L49 and L149 also reference `workspace.Update() → *Changeset`, which is a different API path entirely.

---

## D. Undefined / under-defined terms

### D1 — `[GAP]` `code.go` hardcodes `ModuleSource(".dagmar")` — project module path is not parameterized

**`app/code.go` L42:** `client.ModuleSource(".dagmar")` loads the project module from a hardcoded relative path. When `code()` runs inside the `.dagger` module, this resolves to dagmar's own `.dagmar/` conformance module — **not the target Project's module**. The `source` Directory (the workspace) is passed in, but the project module is loaded from a fixed path. For multi-Project operation (N+1 contexts, ADR-0008), the project module path must come from the Project CR's `moduleRef` or be derived from the workspace. Currently, `code()` would only work when dagmar is dogfooding itself.

### D2 — `[GAP]` `dagger.Connect()` called with no error handling or `Close()`

**`app/code.go` L38:** `client := dagger.Connect()` — no error check, no `defer client.Close()`. Inside a Dagger module function, the function is already executing in a Dagger context; the idiomatic pattern is to use `dag` (the module-level client) rather than creating a new connection. The `dagger.Connect()` call may create a redundant engine session.

---

## E. Referenced-but-missing ADRs

### E1 — `[GAP]` WorkflowSpec redesign needs ADR update or new ADR

The WorkflowSpec change (typed agent refs vs. map, MaxReviseRounds relocation to QualityGateSpec) is an undocumented deviation from ADR-0016 §1. This is a design decision worth recording — either update ADR-0016's field table to match, or add a short ADR-0022 noting the refinement and its rationale (type safety, schema validation).

---

## F. Housekeeping

### F1 — `[HOUSE]` `shellJoin` doubled comment (same as A2)

**`run_controller.go` L480–481:** stale first comment line should be deleted.

### F2 — `[HOUSE]` Stale test name: `TestReconcile_EmptyModuleFunctionIsTerminalFailed`

**`run_controller_test.go`:** the test now triggers `ModuleFunctionOrWorkflowRefRequired` (both empty), not the old `ModuleFunctionRequired` error. The test name + comment ("review-13 HOUSE-4: symmetric with ModuleRef") should be updated to reflect the dual-mode validation.

### F3 — `[HOUSE]` `.agents/skills/dagmar-review/SKILL.md` references `dagmar_review.run()` — not a Prime Agent native pattern

**`.agents/skills/dagmar-review/SKILL.md` L14–18:** the usage example shows `await dagmar_review.run()`, but Prime Agent skills are invoked via `rlm('sub-task')` or document-driven patterns, not a `.run()` wrapper. This could confuse an agent trying to use the skill. The SKILL.md should document the actual invocation path.

---

## Suggested next ADRs (priority order)

1. **ADR-0016 update** — reconcile WorkflowSpec fields: `agents: map` → typed `coderAgentRef`/`reviewerAgentRef`; note `maxReviseRounds` moved to QualityGateSpec. (Covers B1, E1.)
2. **ADR-0021 update** — fix D7 `WithCurrentModule` → `WithMainModule` (C1); fix D2 sketch return type (C2); reconcile `Changeset()` vs `Diff()` naming (B2); update D8 return type for v0.21.8 `Directory.Diff() → *Directory` (C4).
3. **No new ADR needed for `ToolSetPolicy`** — the field is correctly scoped as Phase 2 incremental; just add a `// Phase 2 incremental: not yet read by the controller` note (A3).

---

## Already tracked in seeds

| Seed | Status | Covers |
|---|---|---|
| dagmar-6594 (code() + Changeset()) | closed | B2 (naming drift — seed resolution says Diff, title says Changeset) |
| dagmar-668d (Agent CRD type) | closed | Agent types — implemented |
| dagmar-8b6d (Prompt CRD type) | closed | Prompt types — implemented |
| dagmar-11fe (QualityGate CRD type) | closed | QualityGate types — implemented |
| dagmar-08d4 (Workflow CRD type) | closed | Workflow types — implemented |
| dagmar-05ed (Controller orchestration) | closed | Orchestration state machine — implemented |

## Newly surfaced (observations only — NOT filed as seeds)

| ID | Tag | Finding | Priority |
|---|---|---|---|
| A1 | `[FIX]` | Hardcoded `maxRound := 3` in two places; ignores `QualityGateSpec.MaxReviseRounds` | High |
| A2 | `[FIX]` | Doubled `shellJoin` doc comment | Low |
| A3 | `[GAP]` | `ToolSetPolicy` defined but inert (no controller/code wiring) | Medium |
| B1 | `[SPEC]` | WorkflowSpec diverges from ADR-0016 (typed refs, no MaxReviseRounds) | High |
| B3 | `[SPEC]` | Sub-Runs can't dispatch `code()` — missing `--source` + `--prompt-file` | High |
| C1 | `[FIX]` | ADR-0021 D7 still says `WithCurrentModule()` (6+ stale refs) | Medium |
| C2 | `[FIX]` | ADR-0021 D2 sketch returns `(string, error)` | Low |
| C3 | `[FIX]` | PipelinePhase docstring lists `"gate"` — phase never entered | Low |
| C4 | `[FIX]` | CONTEXT.md Changeset entry wrong about `Diff()` return type | Medium |
| D1 | `[GAP]` | `code.go` hardcodes `ModuleSource(".dagmar")` — not multi-Project | High |
| D2 | `[GAP]` | `dagger.Connect()` no error handling / no `Close()` | Medium |
| F2 | `[HOUSE]` | Stale test name `EmptyModuleFunctionIsTerminalFailed` | Low |
| F3 | `[HOUSE]` | dagmar-review SKILL.md shows non-native `.run()` pattern | Low |
