# ADR-0008: Engine tenancy & Run concurrency

- **Status:** decided
- **Date:** 2026-08-01
- **Resolved in:** seeds dagmar-cbb8
- **Evidence:** docs/research/spike-engine-tenancy.md (cbb8 spike)

## Context

`dagmar-cbb8` asked: Engine cardinality (singleton-per-cluster shared vs per-Project),
Sandbox isolation/quotas across Projects, whether concurrent Runs on one Task are allowed,
and who sequences Workspace lineage — all open sub-questions of the Hybrid-C topology
(ADR-0004). Surfaced by foundations review A2/C4. Maps to Research Q3 (can a single
engine serve multiple isolated agents?) and Q1 (shared-cache poisoning).

The cbb8 spike (see evidence) validated empirically: a Dagger engine runs reliably
in-cluster, and a **separate client pod reaches a singleton engine via `kube-pod://`**
with a ServiceAccount + `pods/exec` RBAC (Q3 ✓). The owner's direction is a singleton
engine.

## Decision

### 1. Engine cardinality = SINGLETON

One Dagger engine (DaemonSet) serves all Projects. Validated feasible in the spike
(engine `1/1 Running Ready`; separate client reached it). Per-Project / per-namespace
engine instances are rejected — no justification warrants the resource cost and
complexity.

### 2. Agent-pod connection = `kube-pod://` + ServiceAccount + `pods/exec` RBAC

Agent pods are clients of the singleton engine via
`_EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://<engine-pod>?namespace=<ns>`. Each Project's
agent pods use a **ServiceAccount with a Role granting `pods/exec`** on the engine pod
(spike F2). The controller provisions the SA + RoleBinding per Project.

### 3. Cache isolation = per-Project cache-volume names

The singleton engine has one shared cache store; Dagger isolates cache by **volume
name**. The controller allocates **distinct cache-volume names per Project**, which gives
cross-Project cache isolation (Research Q1). Cache poisoning across Projects is prevented
as long as Projects never share a cache-volume name.

### 4. Namespaces = optional (defense-in-depth)

Namespaces are **not** required for tenancy — the singleton engine + per-Project
ServiceAccount/RBAC + per-Project cache-volume names already provide isolation. They
remain available as defense-in-depth (blast-radius, NetworkPolicy, ResourceQuota). If
adopted later, use a common prefix `dagmar-<project>` for traceability. (This sharpens
ADR-0007: credential isolation is a Sandbox/encapsulation property; namespaces are tenancy
hygiene, not the credential control.)

### 5. Concurrency & Workspace lineage = controller-level (deferred)

The singleton engine handles concurrent client sessions natively (Dagger's per-session
isolation). Whether concurrent Runs on one Task are allowed, and who sequences Workspace
lineage, are **control-plane** concerns, not engine-tenancy — deferred to the controller
design.

## Alternatives considered

- **Per-Project / per-namespace engines.** Rejected — no warrant for the resource cost and
  operational complexity; the owner cannot justify a separate engine per Project.
- **Namespaces as the primary tenancy boundary.** Rejected for tenancy (singleton engine +
  RBAC + cache-volume names suffice); retained only as optional defense-in-depth.

## Consequences

- **Glossary:** agent pods are `kube-pod://` clients of the singleton engine, each with its
  own ServiceAccount + `pods/exec` RBAC; "cache isolation" = per-Project cache-volume names.
- **ADR-0004:** the in-cluster engine is **singleton**; agent pods are its clients.
- **ADR-0007:** credential-isolation rationale sharpened (encapsulation is the control;
  namespaces optional) — no change to storage/scoping.
- **Operations:** the controller must provision, per Project, (a) a ServiceAccount + a
  `pods/exec` RoleBinding to the engine pod, and (b) a distinct cache-volume name space.
- **Deferred:** concurrent-Runs-on-one-Task policy and Workspace-lineage sequencing
  (control-plane design); an empirical Q1 cache-poisoning test (the design conclusion
  stands; the spike host is kind, production uses dedicated runner nodes per ADR-0004).
