# ADR-0002: Kubernetes CRD boundary

- **Status:** decided
- **Date:** 2026-07-31
- **Resolved in:** seeds dagmar-4271

## Context

dagmar is a Kubernetes control plane (execution model Hybrid-C — see ADR-0004: Go/K8s control plane +
agent pods; Dagger as the hermetic engine). A control plane implies a controller, so
the question is not "CRDs yes/no" but **which entities are custom resources**. A single
CRD is poor ROI for an operator; the decision is the coherent *set* and the boundary
between CR and non-CR.

## Decision

**CRDs:** `{Project, Agent, Prompt, QualityGate, Trigger, Run}`.
**Non-CR:** `{Task, Sandbox, Workspace, ProjectManifest}`.

Boundary principle — an entity is a CRD when its state is **declarative / stable /
reconciled** (definitions, policy, registration) or an **observable execution unit**;
it is **not** a CRD when its state is **canonical elsewhere** or a **runtime
artifact**:

| Entity | CRD? | Reason |
|--------|------|--------|
| Project, Agent, Prompt, QualityGate, Trigger | ✅ | definitions / policy / registration — stable, declarative |
| Run | ✅ | observable, reconciled execution (cf. Tekton `TaskRun`, k8s `Job`); `kubectl get runs` supervises the autonomous system |
| Task | ❌ | canonical in seeds (= the seeds issue); a CR would duplicate it; the controller observes seeds |
| Sandbox | ❌ | runtime artifact — the pod / engine session the controller spawns |
| Workspace | ❌ | runtime artifact — the clone / `CodeWorkspace` |
| ProjectManifest | ❌ | git-native in-repo contract (see ADR-0003) |

## Alternatives considered

- **CRDs for everything (incl. Task, Sandbox, Workspace).** Rejected — a Task CR
  duplicates seeds (which stays canonical); Sandbox/Workspace are high-churn runtime
  artifacts that would noise up etcd.
- **A single `Project` CRD.** Rejected — poor operator ROI; misses the coherent
  definition/policy/registration family (Agent, Prompt, QualityGate, Trigger) that
  justifies the controller, and the observable Run.
- **No CRDs (ConfigMap / file registry).** Rejected for the stable definition/policy
  tier. Noted as a minimal bootstrap fallback only: a ConfigMap registry could seed
  Projects before the controller exists, then migrate to the Project CRD.

## Consequences

- The operator is justified by six coherent custom resources.
- seeds remains the single source of truth for work; the controller observes it rather
  than mirroring it.
- Runs are observable and reconciled (`kubectl`, status, history); Sandboxes/Workspaces
  remain internal runtime state.
