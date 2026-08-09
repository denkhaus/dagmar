# ADR-0023: Prompter-LLM — dynamic prompt synthesis replaces canopy cross-store merge

- **Status:** decided
- **Date:** 2026-08-09
- **Resolved in:** grilling session dagmar-a9d0
- **Supersedes:** ADR-0005 (cross-store merge), ADR-0006 Calibration Agent (replaced by Adjudicator),
  ADR-0017 `dagmar-prompt` hook (removed), ADR-0019 D2/D3 `dagmar-prompt` signature + asymmetry (removed)
- **Evidence:** builds on ADR-0009 (quality-gate workflow), ADR-0016 (orchestration model),
  ADR-0017 (unified hooks), ADR-0021 (loop wrapping), ADR-0022 (env construction)

## Context

ADR-0005 specified a controller-side cross-store merge: canopy CLI (`cn`) renders the project
prompt + dagmar mixins as section sets, a Go layer merges them, and the result is the agent's
prompt file. This approach has three problems:

1. **Hard platform dependency on canopy.** Every agent pod must install bun + `@os-eco/canopy-cli`.
   The platform cannot function without an external prompt-management tool.
2. **Static, mechanical composition.** The merge is section-name → last-wins. It cannot adapt a
   prompt to the specific task — a coder working on a database migration gets the same prompt
   skeleton as one working on a UI bug fix.
3. **Tight project coupling.** The project's `.canopy/` store is a direct dependency. The project
   must maintain canopy prompts that dagmar reads at runtime — a coupling surface that adds
   friction without proportional value.

## Decision

Replace the mechanical cross-store merge with a **Prompter-LLM** — a dedicated LLM step that
synthesizes tailored prompts at runtime by reading project context and the specific task.

### D1 — Prompter step: a dedicated LLM that reads context and writes a prompt

A new `prompt()` function on the `.dagger` module runs a short LLM loop with:

- **Read-only source access** — `Env.WithDirectoryInput("source")`, NOT `Writable`. The prompter
  reads the project tree but does not modify it.
- **Project Hook tools** — `dagmar-issues` (read/search) + `dagmar-memory` (read/search),
  registered via `WithMainModule`. The prompter can read issues and expertise.
- **A static meta-prompt** — embedded via `go:embed` from `internal/prompts/*.md`. The meta-prompt
  tells the LLM *what kind* of prompt to write and which mandatory rules (safety, workflow, output
  format) it must include. One meta-prompt per prompt type: `coder-meta.md`, `reviewer-meta.md`.
- **String output** — the synthesized prompt is the output (not a Directory). The controller
  forwards it as `--prompt-file` to the subsequent coder/reviewer Run.

```go
func (m *Dagmar) Prompt(
    ctx context.Context,
    source       *dagger.Directory,  // read-only project source
    phase        string,             // "pre-code" | "pre-review"
    taskContext  string,             // issue text / task description from dagmar-issues
    model        string,             // separate model per role (ADR-0023 D6)
    maxAPICalls  int,                // short budget — prompt synthesis is bounded
) (string, error)
```

The prompter is a **full `Loop()`** (not single-shot) — it calls `dagmar-issues` and
`dagmar-memory` as tools during synthesis, reads files from source, then produces the prompt.
`MaxAPICalls` is low (e.g. 10) — prompt synthesis is well-bounded.

### D2 — Two separate prompter calls (pre-code, pre-review)

The prompter runs at two points in the pipeline:

1. **Pre-code** — reads the task + context, synthesizes the coder prompt.
2. **Pre-review** — reads the coder's diff + context, synthesizes the reviewer prompt.

These are separate calls, not one call with two outputs. The pre-review call only runs if the
gate is green (a red gate means no review — the coder revises).

### D3 — Prompter→Coder are chained (no controller decision point between them)

The prompter and coder run in the same agent Pod as chained `dagger call` commands:

```
dagger call prompt --source /workspace --phase pre-code ... --output /tmp/prompt.md && dagger call code   --source /workspace --prompt-file /tmp/prompt.md --model ...
```

There is no controller decision point between prompting and coding — the prompter's output flows
directly to the coder. Gate and Reviewer remain separate Sub-Runs (the controller evaluates
gate-green between them).

### D4 — Adjudicator replaces the Calibration Agent

ADR-0006 specified a non-gating Calibration Agent that diagnoses gate↔reviewer disagreement and
emits canopy mixins. This is replaced by the **Adjudicator** — an LLM step that actively resolves
the conflict, not just diagnoses it.

**Trigger:** only on direct disagreement — Gate green + Reviewer veto, or Gate red + Reviewer
approve. Consensus (green+green → merge; red+red → revise) does not trigger the Adjudicator.

**Three resolution paths:**

```
Gate ↔ Reviewer disagreement
                    ↓
        Adjudicator analyzes root cause
              ↙           ↓            ↘
    Reviewer wrong     Gate wrong     Unresolvable
         ↓                 ↓                ↓
  Calibrate reviewer   Coder-Run:        Escalate
  (adjust reviewer     "repair gate"     to human
   meta-prompt or      (strengthen or
   review context)     weaken checkables)
                            ↓
                     Full re-run:
                     gate + review
```

1. **Reviewer wrong** — the reviewer's judgment was incorrect. The Adjudicator adjusts the
   reviewer's context (e.g., additional project-specific guidance in the next pre-review prompt).
   The merge proceeds.
2. **Gate wrong** — the gate's checkables are too weak (missed a real issue) or too strict (flagged
   a non-issue). The Adjudicator starts a new coder-Run instructing the coder to repair the gate
   checkables (Go code in the project repo — ADR-0017 §3). The **Reviewer is the defense** against
   gate manipulation: if the coder weakens the gate to force green, the reviewer sees the full diff
   (including gate changes) and can veto. After the coder repairs the gate, the full pipeline
   re-runs (gate + review).
3. **Unresolvable** — the Adjudicator cannot determine who is right. Escalates to a human.

The Adjudicator always acts in the project's best interest. When it cannot, it escalates.

### D5 — Revised pipeline form

```
[Task from dagmar-issues]
         ↓
[1. Prompter LLM (pre-code)] → tailored coder prompt
         ↓ (chained in same Pod)
[2. Coder LLM] → code changes
         ↓
[3. dagmar-gate (deterministic)]
         ↓ green
[4. Prompter LLM (pre-review)] → tailored reviewer prompt
         ↓ (chained in same Pod)
[5. Reviewer LLM] → approve / veto
         ↓
    green+approve → merge (two-green)
    red+red       → revise loop (new coder Run with feedback)
    disagreement  → [6. Adjudicator LLM] → resolve or escalate
```

Pipeline phases on the orchestration Run status:
`"prompting" | "coding" | "gating" | "reviewing" | "adjudicating" | "escalated" | "done"`

### D6 — Separate models per role

Prompter, Coder, Reviewer, and Adjudicator each have their own Agent CR (with its own model +
budget). The prompter may use a smaller/faster model (well-structured synthesis task); the coder
needs a strong model; the adjudicator needs strong reasoning.

### D7 — WorkflowSpec gains PrompterAgentRef + AdjudicatorAgentRef

```go
type WorkflowSpec struct {
    CoderAgentRef      string `json:"coderAgentRef"`
    PrompterAgentRef   string `json:"prompterAgentRef"`             // NEW — required
    ReviewerAgentRef   string `json:"reviewerAgentRef,omitempty"`
    AdjudicatorAgentRef string `json:"adjudicatorAgentRef,omitempty"` // NEW — optional
    QualityGateRef     string `json:"qualityGateRef"`
    RequiresTwoGreen   bool   `json:"requiresTwoGreen,omitempty"`
}
```

`PrompterAgentRef` is **required** — every LLM workflow needs a prompter.
`AdjudicatorAgentRef` is **optional** — without it, disagreement escalates directly to a human.

### D8 — Prompt CRD and dagmar-prompt hook removed

| Component | Action |
|-----------|--------|
| `Prompt` CRD | **Removed** — the Prompter-LLM replaces it |
| `PromptRef` in Agent.Spec | **Removed** — Agent.Spec retains only Model + MaxAPICalls |
| `dagmar-prompt` hook (ADR-0017/0019) | **Removed** from the required hook set |
| `dagmar-prompt` conformance check (ADR-0019 D4) | **Removed** — one fewer required LLM-Tool hook |
| `Compose()` / `ShellComposeCommand()` | **Eliminated** |
| canopy CLI in agent pod | **Eliminated** |
| ADR-0005 cross-store merge | **Superseded** by this ADR |
| ADR-0006 Calibration Agent | **Replaced** by the Adjudicator |

The remaining required hooks: `dagmar-bootstrap` + `dagmar-gate` (programmatic, always required);
`dagmar-issues` + `dagmar-memory` (LLM-Tool, required when an LLM agent is involved).

### D9 — Meta-prompts live as embedded Markdown files

Static meta-prompts are `internal/prompts/*.md` files, embedded into the Go binary via `go:embed`.
They are version-controlled, diff-friendly, and compile-time-bundled — no runtime file loading.
The `prompt()` function selects the meta-prompt by phase parameter.

The meta-prompt encodes dagmar's mandatory operational rules (safety, workflow, output format).
These rules are **not overridable by the project** — the project provides context (source, mulch,
issue text), not direct prompt content. The Prompter-LLM is the gate that mediates project
influence.

## Alternatives considered

- **Keep ADR-0005 cross-store merge (canopy).** Rejected — hard platform dependency on `cn`,
  static composition that cannot adapt to the task, tight project coupling. Empirically tested
  and reverted (commit 2b0115f).

- **Map-based composition (dagmar-prompt hook with section-map input).** Rejected — Dagger v0.21.8
  codegen rejects map types as function arguments (`unsupported type *types.Map`), even inside
  structs. JSON-string workaround is possible but still a static mechanical merge.

- **In-loop prompting (agent fetches its own prompt via tool during Loop).** Rejected —
  non-deterministic; the agent may or may not fetch the right prompt. Pre-loop synthesis is
  deterministic and verifiable.

- **Single prompter call producing both coder and reviewer prompts.** Rejected — the reviewer
  may never run (gate red). Separate calls avoid wasted work and allow the pre-review prompt to
  incorporate the coder's actual diff.

## Consequences

- **Canopy is eliminated from the platform.** `cn`/bun are no longer agent-pod dependencies.
  Projects may still use canopy internally, but dagmar does not require it.
- **Prompts are now dynamic, task-specific, and LLM-generated.** Quality depends on the
  Prompter-LLM's model and meta-prompt. The meta-prompt is the control surface.
- **Project influence is mediated and bounded.** The project provides context (source, mulch,
  issues), not direct prompt content. The Prompter-LLM is the sole composer.
- **The gate is self-evolving.** The Adjudicator can instruct the coder to repair gate
  checkables, with the reviewer as defense against manipulation.
- **Four Agent roles** (prompter, coder, reviewer, adjudicator) each with their own model/budget.
- **Two fewer ADRs to maintain in their original form** (ADR-0005 superseded, ADR-0006
  Calibration section replaced) and one fewer CRD (Prompt removed).
