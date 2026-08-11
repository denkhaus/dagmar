# ADR-0026: FSM-driven orchestration pipeline

- **Status:** superseded by ADR-0027 (Pipeline-in-Module) — the step-level FSM topology
  (D2/D3/D5) is replaced by a policy-level FSM. The FSM itself (looplab/fsm, D1/D4) is retained.
- **Date:** 2026-08-11
- **Superseded by:** ADR-0027 (Pipeline-in-Module refactoring)
- **Evidence:** replaces the four hand-written advance* functions; resolves ADR-0016 §1
  "a declarative step engine would be speculative generality" (no longer speculative — the
  pipeline has four roles and revise loops). Builds on ADR-0023 D5 (pipeline), ADR-0024
  (per-role tools), ADR-0025 (structured outputs).
- **Why superseded:** the step-level states (coding/gating/reviewing/adjudicating) assumed the
  controller orchestrates each pipeline step as a separate Sub-Run. ADR-0027 moves the step
  orchestration into the Dagger module (CognitionRun custom object). The FSM remains, but at
  policy level (dispatching/running/done/escalated) — see ADR-0027 D4.

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

### D3 — The FSM topology replaces hardcoded advance* logic (SUPERSEDED by ADR-0027 D4)

> **Superseded:** This step-level topology assumed the controller creates separate Sub-Runs
> per pipeline step. ADR-0027 moves step orchestration into the CognitionRun Dagger custom
> object. The FSM retains looplab/fsm (D1) and requeue-not-recursion (D4), but transitions
> to a policy-level topology: `dispatching → running → done` / `running → escalated`.

Original topology (for historical reference):

```
coding → gating → reviewing → done
   ↑       ↓        ↓
 revise  revise   adjudicating → done
                      ↓
                escalated (unresolvable)
```

The FSM defined all legal transitions. The generic reconcile function observed Sub-Run
outcomes and fired the appropriate event.

### D4 — Requeue, not recursion

When the FSM transitions to a non-terminal state, the reconcile function returns
`ctrl.Result{Requeue: true}` instead of recursing. This is the K8s-idiomatic pattern: each
transition is a separate reconcile cycle, triggered by the controller-runtime work queue.
Recursion caused infinite loops in testing (stale local state) and would mask errors in
production (no backoff between steps).

### D5 — subRunConfigForState is the dispatch table (SUPERSEDED by ADR-0027)

> **Superseded:** With step orchestration moved into the CognitionRun pipeline (ADR-0027),
> the controller no longer creates per-step Sub-Runs. subRunConfigForState is removed.

Original: a single function mapped each state to its Sub-Run configuration (agent ref,
module function, prompter phase, coverage floor). States that didn't create Sub-Runs
(StateGating evaluates the coder's gate result; StateAdjudicating without an adjudicator
escalates) returned nil.

## Consequences (partially superseded by ADR-0027)

- **~~The four advance* functions are replaced~~** by one generic `reconcileOrchestrationFSM` +
  a declarative FSM definition. → **Superseded:** ADR-0027 moves step orchestration into
  the CognitionRun pipeline. The advance* functions and `subRunConfigForState` are removed.
  The FSM remains at policy level.
- **ADR-0016 §1 is updated:** "speculative generality" is no longer speculative. The FSM
  IS the step engine, but at the Go-code level, not in the CRD YAML. → **Still valid**,
  but the FSM is now policy-level (dispatching/running/done/escalated), not step-level.
- **looplab/fsm v1.0.3** is retained as a direct dependency. → **Still valid.**
- **Mermaid visualization** is available via `fsm.VisualizeForMermaidWithGraphType`. → **Still valid.**
- **Future: event-driven extensions** — the FSM supports callbacks and guards. → **Still valid.**
