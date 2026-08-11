# ADR-0026: FSM-driven orchestration pipeline

- **Status:** decided
- **Date:** 2026-08-11
- **Evidence:** replaces the four hand-written advance* functions; resolves ADR-0016 §1
  "a declarative step engine would be speculative generality" (no longer speculative — the
  pipeline has four roles and revise loops). Builds on ADR-0023 D5 (pipeline), ADR-0024
  (per-role tools), ADR-0025 (structured outputs).

## Context

The orchestration controller had four hand-written state-transition functions
(advanceCoding, advanceGating, advanceReviewing, advanceAdjudicating), each following the
same 6-step pattern: resolve prompter, create Sub-Run, wait for completion, read output,
evaluate condition, transition. This is a state machine expressed as duplicated imperative
code. Adding a new pipeline step required copying and modifying an entire function.

ADR-0016 §1 deferred a declarative step engine as "speculative generality" when only one
pipeline form existed. Since then, four LLM roles (prompter, coder, reviewer, adjudicator),
structured JSON outputs (ADR-0025), and per-role tool policies (ADR-0024) have made the
pipeline complex enough to warrant a proper state machine.

## Decision

### D1 — looplab/fsm as the orchestration engine

The pipeline is expressed as a declarative finite-state machine using [looplab/fsm](https://github.com/looplab/fsm)
v1.0.3. The FSM defines states and event-driven transitions; a single generic
`reconcileOrchestrationFSM` function replaces the four `advance*` functions.

K8s remains the sole durable state store (etcd via CRD status). The FSM is reconstructed
on every reconcile from the persisted `PipelinePhase` — consistent with controller-runtime
reconciliation semantics. No second database or workflow engine.

### D2 — All FSM identifiers are constants

States and event names are defined as Go constants in `pipeline_fsm.go`:

```go
const (
    StateCoding       = "coding"
    StateGating       = "gating"
    StateReviewing    = "reviewing"
    StateAdjudicating = "adjudicating"
    StateEscalated    = "escalated"
    StateDone         = "done"
)

const (
    EventCoderSucceeded    = "coder_succeeded"
    EventCoderFailed       = "coder_failed"
    EventGateGreen         = "gate_green"
    EventGateRed           = "gate_red"
    ...
)
```

No inline strings anywhere in the codebase. All transitions reference these constants.

### D3 — The FSM topology replaces hardcoded advance* logic

```
coding → gating → reviewing → done
   ↑       ↓        ↓
 revise  revise   adjudicating → done
                      ↓
                escalated (unresolvable)
```

The FSM defines all legal transitions. The generic reconcile function observes Sub-Run
outcomes and fires the appropriate event. The FSM computes the transition; callbacks are
not needed (the reconcile function handles the post-transition work directly).

### D4 — Requeue, not recursion

When the FSM transitions to a non-terminal state, the reconcile function returns
`ctrl.Result{Requeue: true}` instead of recursing. This is the K8s-idiomatic pattern: each
transition is a separate reconcile cycle, triggered by the controller-runtime work queue.
Recursion caused infinite loops in testing (stale local state) and would mask errors in
production (no backoff between steps).

### D5 — subRunConfigForState is the dispatch table

A single function maps each state to its Sub-Run configuration (agent ref, module function,
prompter phase, coverage floor). States that don't create Sub-Runs (StateGating evaluates
the coder's gate result; StateAdjudicating without an adjudicator escalates) return nil.

## Consequences

- **The four advance* functions are replaced** by one generic `reconcileOrchestrationFSM` +
  a declarative FSM definition. Adding a pipeline step = adding an FSM event + transition +
  a case in `subRunConfigForState`.
- **ADR-0016 §1 is updated:** "speculative generality" is no longer speculative. The FSM
  IS the step engine, but at the Go-code level, not in the CRD YAML.
- **looplab/fsm v1.0.3** is a new direct dependency (3.4k stars, Apache 2.0, actively used).
- **Mermaid visualization** is available via `fsm.VisualizeForMermaidWithGraphType` — the
  pipeline can be rendered as a diagram for documentation/debugging.
- **Future: event-driven extensions** — the FSM supports callbacks (before_/after_/enter_/leave_)
  and guards. External events (human approval, timer expiry) can fire FSM events without
  changing the reconcile structure.
