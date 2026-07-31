# ADR-0004: Execution topology (Hybrid-C)

- **Status:** decided (topology); open sub-questions tracked separately
- **Date:** 2026-07-31
- **Resolved in:** seeds `dagmar-80dd` (wayfinder map) + foundations review C1

## Context

dagmar must both **orchestrate** durable, multi-repo, proactive work (a control plane:
CRDs, scheduling, triggers, cron housekeeping) and **execute** hermetic, cached,
self-verifying agent runs. The wayfinder map chose "Hybrid (C)" but the A/B/C
alternatives and rationale were never recorded — even though ADR-0002 (CRD boundary)
already presupposes a Kubernetes control plane.

## Decision

Execution topology = **Hybrid-C**: a Go/Kubernetes control plane (CRDs + controller,
scheduling, triggers, proactive cron) **plus** agent pods that invoke an in-cluster
Dagger engine for hermetic code-changing actions and as the LLM/tool sandbox
(`dag.LLM().Loop()` on a `CodeWorkspace`).

Dagger-in-K8s per `docs/research/dagger-in-k8s.md` (full findings + sources): engine
deployed as a Helm DaemonSet; agent pods via
`_EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://`; 3-layer cache with optional S3/GCS
backend.

## Alternatives considered

- **A — Pure Dagger.** dagmar implemented as Dagger module(s); no K8s control plane;
  the engine does everything. Rejected — no durable control plane for scheduling,
  multi-repo fan-out, or proactive cron (Dagger is invocation-based).
- **B — Pure Kubernetes.** Standard controllers / Jobs / Pods calling the LLM
  directly, no Dagger engine. Rejected — loses hermetic/cached execution, the
  `dag.LLM().Loop()` primitive, and `CodeWorkspace` self-verification.
- **C — Hybrid (chosen).** K8s control plane for orchestration + Dagger engine for
  hermetic execution/sandboxing. Combines durable orchestration with Dagger's agent
  primitive.

## Consequences

- ADR-0002's CRD boundary and the controller/`Run` model follow from this topology.
- **Open sub-questions** (filed as seeds): Engine tenancy & cross-Project isolation
  (singleton vs per-Project; Sandbox quotas); Run concurrency / scheduling /
  Workspace-lineage sequencing.
