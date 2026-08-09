# ADR-0016: Workflow-CRD framework (general pipelines)

- **Status:** decided
- **Date:** 2026-08-08
- **Resolved in:** seed dagmar-ff60
- **Evidence:** grilling session 2026-08-08 (seed `dagmar-ff60`); builds on ADR-0009 (quality-gate workflow family), ADR-0002 (CRD boundary), ADR-0006 (autonomy model / two-green), ADR-0012 (self-bootstrap trajectory / phase model), ADR-0013 (control-plane design).

## Context

ADR-0009 reframed dagmar as a **Dagger-based software factory** and introduced the Workflow
concept: "a thin CRD referencing a Dagger Go function (pipeline logic, NOT YAML steps)." The
quality-gate family (ADR-0009 §4) is **one instance** of a workflow. The general framework was
deferred to this ADR — it fixes the CRD shape, the orchestration model, and the contracts that
Phase 2 (cognition) and Phase 3 (autonomy) build on.

The existing CRD set is `{Project, Agent, Prompt, QualityGate, Trigger, Run}` (ADR-0002). `Run`
is the atomic execution unit: one Agent, one Sandbox, one Workspace (ADR-0013). Today the
controller reconciles a Run by spawning an agent pod that calls a single Dagger function
(`spec.moduleFunction` + `spec.moduleArgs`). The quality-gate pipeline (gate → review → merge
with revise loop) is a **multi-Run orchestration** — it needs a layer above the atomic Run.

## Decision

### 1. Workflow is a new CRD — a pipeline template referencing Dagger functions + controller-interpreted orchestration metadata

A Workflow is **not** a step DSL (no YAML pipeline steps). It is a function reference **plus**
workflow-level metadata that the controller interprets. The pipeline logic (gate → review →
merge) lives in the controller as a hardcoded pipeline form per workflow type — not in the CRD
as a declarative step graph (only one pipeline form exists today; a declarative step engine
would be speculative generality).

**WorkflowSpec fields:**

| Field | Type | Description |
|-------|------|-------------|
| `qualityGateRef` | `string` | References the QualityGate CR by name (gate policy = what is checked). |
| `agents` | `map[string]string` | Role → Agent CR name. Roles are pipeline-type-specific: the quality-gate family uses `coder`, `reviewer`. |
| `maxReviseRounds` | `int` | Maximum coder-revise iterations before escalating to a human (ADR-0009 §4). Default 3. |
| `requiresTwoGreen` | `bool` | Enforces `QualityGate.green ∧ ReviewAgent.approve` (ADR-0006). When true, the controller gates delivery on both signals. |

**Workflow has no Status subresource** — it is a pure definition/template (like Agent, Prompt,
QualityGate). Orchestration state lives on the orchestration Run (§3).

### 2. Run is dual-mode: atomic (existing) or orchestration (new)

Run.spec gains one optional field, mutually exclusive with `moduleFunction`:

```go
type RunSpec struct {
    ProjectRef     string   `json:"projectRef"`
    // Atomic mode (existing): one function, one agent pod.
    ModuleFunction string   `json:"moduleFunction,omitempty"`
    ModuleArgs     []string `json:"moduleArgs,omitempty"`
    // Orchestration mode (new): references a Workflow template.
    // Mutually exclusive with ModuleFunction.
    WorkflowRef    string   `json:"workflowRef,omitempty"`
    // Sub-Runs only: the orchestration Run that created this atomic Run.
    // Empty on atomic Runs created directly and on orchestration Runs.
    // +optional
    ParentRun      string   `json:"parentRun,omitempty"`
}
```

- **Atomic Run** (`ModuleFunction` set): existing behavior — controller spawns one agent pod,
  calls one Dagger function.
- **Orchestration Run** (`WorkflowRef` set): controller reads the Workflow template and
  orchestrates 1..N atomic Sub-Runs (gate, coder, review) in the pipeline form hardcoded for
  that workflow type. Merge is a controller action (§5), not a Sub-Run.

Sub-Runs carry `ModuleFunction` (the concrete Dagger function) and a `parentRun` reference
linking them to their orchestration Run.

### 3. Orchestration Run status: dedicated pipeline-tracking fields

The orchestration Run's Status gains fields that let the controller reconstruct pipeline state
after a restart/crash (K8s reconciliation):

```go
type RunStatus struct {
    // Existing (atomic Runs):
    Phase       string            `json:"phase,omitempty"`
    AgentPodName string           `json:"agentPodName,omitempty"`
    Conditions  []metav1.Condition `json:"conditions,omitempty"`
    // New (orchestration Runs only):
    PipelinePhase  string   `json:"pipelinePhase,omitempty"`   // "gate" | "coder" | "review" | "calibration" | "escalated" | "done"
    // "calibration" is reached only when the Calibration Agent exists (ADR-0006, deferred).
    // "escalated" is the terminal state when maxReviseRounds is exhausted (human handoff).
    // "done" = Two-Green reached and merge completed.
    CurrentRound   int      `json:"currentRound,omitempty"`    // revise-round counter
    SubRunRefs     []string `json:"subRunRefs,omitempty"`      // names of child Runs
}
```

These fields are zero-valued on atomic Runs.

### 4. Orchestration is controller-driven (not function-driven)

The controller is the orchestrator. It reads the Workflow metadata (`requiresTwoGreen`,
`maxReviseRounds`, `agents`, `qualityGateRef`), generates Sub-Runs in the pipeline sequence, and
evaluates their status to decide transitions (RED → revise, GREEN → review, VETO → calibration).

Each Sub-Run's Dagger function is **convention, not configuration** — the controller knows:

| Pipeline step | Dagger function | Source |
|---------------|-----------------|--------|
| Gate | `dagmar-gate` | ADR-0009 §2 / ADR-0012 §4 |
| Bootstrap | `dagmar-bootstrap` | ADR-0009 §2 / ADR-0012 §4 |
| Coder | (Phase 2 — coder-loop function) | ADR-0012 §5 Phase 2 |
| Review | (Phase 2 — review-agent function) | ADR-0009 §6 |

A Project participating in a workflow must expose the required functions as Dagger module
exports (conformance contract, ADR-0009 §2).

### 5. Merge is a controller action, not a pipeline step

On Two-Green (`QualityGate.green ∧ ReviewAgent.approve`), the controller merges the PR directly.
Merge is a privileged controller command (git merge / API call using the Project's credentials),
not an agent Run — no LLM, no Sandbox, no Workspace. It is the terminal action of the
orchestration, not a Sub-Run.

### 6. Trigger → Workflow binding: `Trigger.spec.workflowRef`

A Trigger references a Workflow by name. When the event fires, the controller creates a Task
(seeds issue, ADR-0013 §3) and an orchestration Run (`spec.workflowRef` set) under that Task.
The orchestration Run's Sub-Runs inherit the `taskRef` for lineage tracking. The event-filter
logic (which events fire the trigger) stays on the Trigger — it is an event condition, not a
pipeline property. Full Trigger semantics (webhook/cron/manual) are Phase 3.

```yaml
# Example (Phase 3 — Trigger not yet implemented):
apiVersion: dagmar.denkhaus.io/v1alpha1
kind: Trigger
spec:
  workflowRef: quality-gate-family
  projectRef: dagmar-own
  # Event filters (Phase 3):
  events: ["pull_request"]
```

### 7. CRD set extension

The canonical CRD set (ADR-0002) grows from `{Project, Agent, Prompt, QualityGate, Trigger,
Run}` to `{Project, Agent, Prompt, QualityGate, Trigger, **Workflow**, Run}`. Workflow occupies
the definition/policy layer alongside Agent, Prompt, and QualityGate.

### 8. QualityGate stays a standalone CRD

The QualityGate CRD defines *what* the gate checks (checkables, rules, thresholds) — **declarative**
policy. The Workflow references it (`spec.qualityGateRef`). The separation is:

- **QualityGate** = what is checked (policy)
- **`dagmar-gate`** = how it is invoked (Dagger function, convention)
- **Workflow** = when and in which pipeline context it runs (orchestration template)

### 9. Scope: design decision only — no implementation

This ADR defines the CRD types, the orchestration model, and the contracts. It does NOT
implement:
- The Go types (api/v1alpha1/workflow_types.go) — Phase 2.
- The controller orchestration logic — Phase 2.
- The Agent/Prompt CRD types (coder, reviewer roles) — Phase 2.
- The coder-loop / review Dagger functions — Phase 2.
- The Trigger CRD + event/webhook/cron model — Phase 3.

## Consequences

- **ADR-0002:** CRD set extended to include Workflow.
- **CONTEXT.md:** Workflow graduates from "forthcoming" to a decided CRD with fields and
  semantics. Glossary entry added.
- **ADR-0009:** the quality-gate family is now one instance of the Workflow-CRD framework. The
  pipeline form (gate → review → merge, revise loop, two-green, calibration) is the hardcoded
  form the controller implements for this workflow type.
- **ADR-0006:** two-green and merge-authority are enforced by the controller (orchestration
  Run), not by an agent or function.
- **ADR-0013:** Run gains a dual-mode (atomic/orchestration). The controller reconciles both;
  orchestration Runs produce and supervise atomic Sub-Runs.
- **Phase 2** implements: Workflow + Agent + Prompt CRD types, coder-loop function, the
  controller orchestration logic, and the first concrete Workflow instance (quality-gate family).
  **Implementation refinement (Review 27 B1):** `WorkflowSpec.agents` map replaced by typed
  `coderAgentRef` + `reviewerAgentRef` fields (schema validation, clearer for the quality-gate
  family); `maxReviseRounds` moved from WorkflowSpec to QualityGateSpec.
- **Phase 3** implements: Trigger CRD, event/webhook/cron model, and reactive/proactive dispatch.

## Alternatives considered

- **Declarative step engine (Workflow as a YAML step graph with transitions).** Rejected — only
  one pipeline form exists today (quality-gate family). A step DSL is speculative generality.
  The controller hardcodes the form; a second form can add a second workflow type later.
- **Function-driven orchestration (the Dagger function orchestrates internally).** Rejected —
  contradicts the K8s reconciliation model. The control plane should own orchestration, not a
  Sandbox function. Debugging pipeline state via `kubectl get runs` requires controller-owned
  state.
- **WorkflowRun as a separate CRD (alongside Run).** Rejected — Run is already "observable,
  reconciled execution unit" (ADR-0002). A second execution CRD for the same role (orchestration)
  dilutes the model. Dual-mode Run keeps one execution CRD.
- **Merge as a Sub-Run.** Rejected — merge is a privileged controller action (credentials, K8s
  power), not an agent job (no LLM, no Sandbox, no Workspace).
- **Function names configurable in the Workflow.** Rejected — `dagmar-gate` and
  `dagmar-bootstrap` are already convention (ADR-0009). Making them per-Workflow configurable
  adds a mapping table for one pipeline form.
- **Workflow-level Status.** Rejected — Workflow is a template (definition layer). Status lives
  on the orchestration Run (execution layer), consistent with Agent/Prompt/QualityGate having no
  reconcile status.
