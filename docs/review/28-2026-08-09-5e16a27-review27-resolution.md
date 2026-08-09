# Review 28 — Resolution of Review 27 Findings

- **Scope:** `7a78c79..5e16a27` — Resolution commit addressing Review 27 findings (A1–A3, B1–B3, C1–C4, D1–D2, F1–F3). Two commits: skill `_rlm` injection + doc/code/ADR corrections.
- **Baseline:** `5e16a27`
- **Changed files (13):** `dagmar_review.py` (_rlm param), `dagger.gen.go` (regen for moduleRef), `code.go` (moduleRef + Connect refactor), `main.go` (moduleRef param), `CONTEXT.md` (Changeset entry), `agent_types.go` (Phase 2 note), `run_types.go` (PipelinePhase docstring), ADR-0016 (WorkflowSpec refinement), ADR-0021 (D7 title + D8 rename), orchestration.go (maxReviseRoundsFor helper), run_controller.go (shellJoin comment), run_controller_test.go (test rename), Review 27 report (added).
- **Tag legend:** `[FIX]` contradiction/standards breach, fix now · `[GAP]` referenced but undecided, needs ADR/glossary · `[HOUSE]` doc structure · `[SPEC]` deviation from the seed/ADR ask

---

## A. Standards

The resolution addresses 8 of 16 Review 27 findings correctly and 4 partially. Build/vet/test all green (`./...` + `.dagger/...`, 10 tests pass). However, three corrections introduced new problems:

1. **Dead code** — `maxReviseRoundsFor()` was written to resolve A1 but is never called; the hardcoded `maxRound := 3` persists in both call sites. (→ A1 below)
2. **D2 regression** — the `client := dagger.Connect()` refactor replaced a single call with three inline `dagger.Connect()` calls, each with no error handling or `Close()`. (→ A2 below)
3. **Partial ADR updates** — ADR-0021 D7 title was changed but body text (5 refs) was left stale; D8 function was renamed but return type/comment/Consequences still say `Changeset`. (→ B2, C1 below)

Fowler smells:

- **Duplicated Code** — `dagger.Connect()` now appears 3× inline in `code.go` (L41, L46, L50) where a single `client := dagger.Connect()` (or `dag`) would suffice. Each is an unguarded, unclosed connection. → A2
- **Dead Code** — `maxReviseRoundsFor()` is a well-written helper that no call site invokes. → A1
- **Divergent Change** — ADR-0021 was partially updated (D7 title, D8 function name) but the rest of the document was not swept, creating internal contradictions within the same ADR. → B2, C1

### A1 — `[FIX]` `maxReviseRoundsFor()` written but never called — A1 resolution incomplete

**`orchestration.go` L234–250:** the new `maxReviseRoundsFor()` method correctly reads `QualityGateSpec.MaxReviseRounds` from the referenced QualityGate, with sensible defaults. However, `advanceCoder` (L100) and `advanceReview` (L138) still have:

```go
maxRound := 3 // default; could read from QualityGate spec
```

The helper exists alongside the unchanged hardcoded value. This is worse than the original A1 finding: the code now pretends to support QualityGate-driven rounds but silently ignores them. The helper should be wired in: `maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)`.

### A2 — `[FIX]` `dagger.Connect()` called 3× inline — D2 regressed

**`code.go` L41, L46, L50:** the original code had `client := dagger.Connect()` once (Review 27 D2: no error check, no `Close()`). The resolution removed the local variable and inlined `dagger.Connect()` three separate times:

```go
projectMod, err := dagger.Connect().ModuleSource(moduleRef).AsModule().Sync(ctx)
env := dagger.Connect().Env().WithWorkspace(source).WithMainModule(projectMod)
llm := dagger.Connect().LLM(dagger.LLMOpts{...})
```

Each call may create a new engine session. The original D2 concern (missing error handling / `Close()`) was not addressed and the problem multiplied 3×. Inside a Dagger module function, the idiomatic pattern is to use the module-level `dag` client (no `Connect()` needed). If `Connect()` must be used, it should be called once with error handling and `defer client.Close()`.

---

## B. Spec

### B1 — `[FIX]` ADR-0021 D1 code sketch stale — missing `moduleRef` parameter

**ADR-0021 L66–82 (D1):** the D1 code sketch shows:

```go
func (m *Dagmar) Code(ctx, source, promptFile, model, maxAPICalls) (*dagger.Directory, error) {
    return app.Code(ctx, source, promptFile, model, maxAPICalls)
}
```

The actual implementation now has `moduleRef` as a 6th parameter (`main.go` L112–116, `+default=".dagmar"`). D1's sketch was not updated alongside the D1 fix (moduleRef addition). The Consequences section (L247) also still says `Code(source, promptFile, model, maxAPICalls) → *dagger.Directory`.

### B2 — `[FIX]` ADR-0021 D8 partially renamed — return type + comments still stale

**ADR-0021 L225–232:** the D8 function was renamed `Changeset` → `Diff` in the Go code block, but:

- **L225 comment:** still says "Changeset extracts the diff" — should say "Diff extracts".
- **L232 return type:** `(*dagger.Changeset, error)` — the actual `app.Diff` returns `*dagger.Directory` (v0.21.8 `Directory.Diff() → *Directory`). The function is now named `Diff` but typed as `*Changeset`, creating a name-type contradiction within the same block.
- **L247 Consequences:** still says `Changeset(before, after) → *dagger.Changeset` — not updated.

This is a partial B2 resolution that introduced a new internal contradiction.

---

## C. Inconsistencies / contradictions

### C1 — `[FIX]` ADR-0021 body still says `WithCurrentModule()` (5 stale refs)

**ADR-0021 D6 L192, D7 L202 + L211, Consequences L259, Alternatives L286:** the D7 title was corrected to "WithMainModule", but five body-text references to `WithCurrentModule()` remain:

| Line | Section | Text |
|------|---------|------|
| L192 | D6 | "includes `WithCurrentModule()`, which registers the project module's functions as tools" |
| L202 | D7 | "Env includes `WithCurrentModule()` (registers the LLM-Tool hooks)" |
| L211 | D7 | "all coder Runs are hermetic: `WithCurrentModule()` only" |
| L259 | Consequences | "Hermetic by default: `WithCurrentModule()` only" |
| L286 | Alternatives | "The Env's tools come from `WithCurrentModule()` (always)" |

Note: L36 (API surface) and L81 (D2 correction note) correctly reference `WithCurrentModule()` — the former documents the real v0.21.8 API, the latter explains why not to use it. These are fine.

### C2 — `[FIX]` ADR-0021 D2 sketch return type still `(string, error)` — R27 C2 not addressed

**ADR-0021 L93:** the D2 "sketch" still shows `func Code(...) (string, error)` while the actual signature returns `(*dagger.Directory, error)`. The sketch also still hardcodes `.dagmar` at L96 (`ModuleSource(".dagmar")`) even though D1 was updated to use `moduleRef`.

---

## D. Undefined / under-defined terms

No new terms introduced. The `moduleRef` parameter added to `Code()` is well-documented in code comments and matches ADR-0014's terminology (Project CR `moduleRef`). No glossary update needed.

---

## E. Referenced-but-missing ADRs

No new ADRs needed. The ADR-0016 refinement note (B1/E1) adequately documents the WorkflowSpec field redesign.

---

## F. Housekeeping

### F1 — `[HOUSE]` SKILL.md still shows `dagmar_review.run()` — R27 F3 not addressed

**`.agents/skills/dagmar-review/SKILL.md` L14, L17:** the usage examples still show `await dagmar_review.run()`. The `_rlm` parameter was added to `dagmar_review.py` (commit 647e71c) but the SKILL.md invocation pattern was not corrected. This is the same F3 finding from Review 27 — the skill documents a non-native `.run()` call pattern.

### F2 — `[HOUSE]` ADR-0021 D2 sketch stale forward-pointer to `.dagmar`

**ADR-0021 L96:** the D2 sketch still says `ModuleSource(".dagmar")`. Now that `moduleRef` is a parameter, the sketch should show `ModuleSource(moduleRef)` to match the implementation.

---

## Suggested fixes (priority order)

1. **Wire `maxReviseRoundsFor()` into both call sites** — replace `maxRound := 3` with `maxRound := r.maxReviseRoundsFor(ctx, wf, run.Namespace)` in `advanceCoder` and `advanceReview`. (A1 — highest priority: dead code pretending to work.)
2. **Collapse `dagger.Connect()` to a single call** — either use the module-level `dag` client (idiomatic inside a module), or `client, err := dagger.Connect(); defer client.Close()` once at the top of `Code()`. (A2 — D2 regression.)
3. **Sweep ADR-0021 for remaining `WithCurrentModule()` → `WithMainModule`** — fix L192, L202, L211, L259, L286. (C1.)
4. **Fix ADR-0021 D8 return type** — `*dagger.Changeset` → `*dagger.Directory` at L232; update L225 comment and L247 Consequences. (B2.)
5. **Fix ADR-0021 D1 + D2 sketches** — add `moduleRef` to D1 signature; fix D2 return type `(string, error)` → `(*dagger.Directory, error)`; replace `.dagmar` with `moduleRef`. (B1, C2.)
6. **Update SKILL.md invocation pattern** — document the actual Prime Agent invocation path. (F1.)

---

## Review 27 finding resolution tracker

| R27 ID | Tag | Status | Detail |
|--------|-----|--------|--------|
| A1 | FIX | **INCOMPLETE** | `maxReviseRoundsFor()` added but never called; both call sites still hardcode `maxRound := 3` |
| A2 | FIX | **RESOLVED** | Stale `shellJoin` comment line removed |
| A3 | GAP | **RESOLVED** | Phase 2 incremental doc note added to `ToolSetPolicy` |
| B1 | SPEC | **RESOLVED** | ADR-0016 refinement note added |
| B2 | SPEC | **PARTIALLY** | D8 function renamed; D8 return type + comment + Consequences still say `Changeset` |
| B3 | SPEC | **NOT ADDRESSED** | Sub-Run dispatch still incomplete (no `--source` / `--prompt-file`) |
| C1 | FIX | **PARTIALLY** | D7 title fixed; 5 body-text `WithCurrentModule()` refs remain |
| C2 | FIX | **NOT ADDRESSED** | D2 sketch still `(string, error)`, still `.dagmar` |
| C3 | FIX | **RESOLVED** | PipelinePhase docstring corrected |
| C4 | FIX | **RESOLVED** | CONTEXT.md Changeset entry corrected |
| D1 | GAP | **RESOLVED** | `moduleRef` parameter added and threaded through |
| D2 | GAP | **REGRESSED** | Single `Connect()` → 3× inline `Connect()`, no error handling or `Close()` |
| E1 | GAP | **RESOLVED** | Same as B1 |
| F1 | HOUSE | **RESOLVED** | Same as A2 |
| F2 | HOUSE | **RESOLVED** | Test renamed to `NeitherModuleFunctionNorWorkflowRef` |
| F3 | HOUSE | **NOT ADDRESSED** | SKILL.md still shows `.run()` pattern |

**Scorecard:** 8 resolved · 3 partially resolved · 3 not addressed · 1 regressed · 1 incomplete.

---

## Already tracked in seeds

| Seed | Status | Covers |
|------|--------|--------|
| dagmar-80dd (Wayfinder map) | open | Epic overview — no specific finding |

## Newly surfaced (observations only — NOT filed as seeds)

| ID | Tag | Finding | Priority |
|----|-----|---------|----------|
| A1 | `[FIX]` | `maxReviseRoundsFor()` dead code — written but never called; hardcoded `maxRound := 3` persists | High |
| A2 | `[FIX]` | `dagger.Connect()` 3× inline — D2 regressed; no error handling or `Close()` | High |
| B1 | `[FIX]` | ADR-0021 D1 sketch missing `moduleRef` parameter | Medium |
| B2 | `[FIX]` | ADR-0021 D8: function renamed to `Diff` but return type still `*Changeset` — name/type contradiction | Medium |
| C1 | `[FIX]` | ADR-0021 body: 5 stale `WithCurrentModule()` references (D6, D7 ×2, Consequences, Alternatives) | Medium |
| C2 | `[FIX]` | ADR-0021 D2 sketch: return type still `(string, error)`, still hardcodes `.dagmar` | Low |
| F1 | `[HOUSE]` | SKILL.md still shows `dagmar_review.run()` — R27 F3 not addressed | Low |
| F2 | `[HOUSE]` | ADR-0021 D2 sketch stale: `.dagmar` should be `moduleRef` | Low |
