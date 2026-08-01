# Spike: Engine tenancy & Run concurrency (cbb8)

- **Date:** 2026-08-01
- **Seed:** dagmar-cbb8 (Engine tenancy & Run concurrency) — open sub-questions of ADR-0004.
- **Status:** core question answered. Q3 (singleton engine serves a client via `kube-pod://`) **validated empirically**. Q1 (cache isolation) = design conclusion (per-project cache volume names), optional empirical follow-up.
- **Artifacts:** dagmar's `.dagger/` module (k3s path: `Up`/`DeployEngine`/`Probe`); `spike-engine-tenancy-q3-client.yaml` (RBAC + client pod for the kind path).

## Goal

Validate dagmar's Hybrid-C in-cluster engine concretely: can a Dagger engine run nested and **serve multiple Projects** (singleton vs per-namespace)? Resolve Research Q3 (single engine serving isolated agents) and Q1 (shared-cache poisoning).

## What was tried

1. **k3s-in-Dagger (dogfood path)** via `github.com/marcosnils/daggerverse/k3s@k3s/v0.11.1`. k3s comes up reliably; the engine installs; **but the k3s flannel CNI is flaky nested** (`/run/flannel/subnet.env: no such file` → pod sandbox fails). Engine reached `1/1 Ready` once; unreliable otherwise. Image caching solved here via `dag.K3S(name, K3SOpts{KeepState: true})` (cold 11.5 min → warm 17 s).
2. **kind (host-level)** — chosen for reliability. kindnetd CNI is stable in Docker; the cluster persists across calls; `kind load docker-image` preloads from Docker's cache. Engine reaches `1/1 Running Ready` reliably.

## Findings

### F1 — A Dagger engine runs reliably nested (kind)
`helm install dagger oci://registry.dagger.io/dagger-helm --set privileged=true` into kind → privileged DaemonSet (`registry.dagger.io/engine:v0.21.8`) reaches `1/1 Running Ready` (0 restarts). Confirms Hybrid-C in-cluster engine feasibility. (k3s-in-Dagger is the same conclusion but flaky due to flannel.)

### F2 — Q3 VALIDATED: a singleton engine serves a separate client pod via `kube-pod://`
A separate client pod (alpine + dagger CLI from the GitHub release tarball + kubectl, with a `ServiceAccount` + `Role`/`RoleBinding` granting `pods/exec`) set `_EXPERIMENTAL_DAGGER_RUNNER_HOST=kube-pod://<engine-pod>?namespace=dagmar`, built an in-cluster kubeconfig from its SA token, and ran `dagger core version` → **`v0.21.8`** (connected to the singleton engine, pod `Succeeded`).

**Conclusion (Q3):** a single Dagger engine CAN serve a separate agent pod via `kube-pod://`, given the client has a ServiceAccount with `pods/exec` RBAC on the engine pod. dagmar's agent-pod model is feasible on a singleton engine. Manifest: `spike-engine-tenancy-q3-client.yaml`.

### F3 — Q1 (cache isolation): design conclusion
The singleton engine is one pod with one shared hostPath cache store. Dagger isolates cache by **volume name**, so cross-Project cache isolation = the controller allocating **distinct cache-volume names per Project** (not namespaces). Cache poisoning across Projects is prevented as long as Projects don't share a cache-volume name. (Optional empirical test deferred.)

## Tenancy direction (decided with owner)

- **Singleton engine** — one Dagger engine serves all Projects. ✓ validated feasible (F1, F2).
- **Per-Project cache isolation via cache-volume names** (F3), not via namespaces.
- **Namespaces optional** (defense-in-depth: blast-radius, NetworkPolicy, ResourceQuota). Owner stays open to multi-namespace with a common prefix `dagmar-<project>` if a decisive advantage appears.
- **Agent pods need a ServiceAccount + `pods/exec` RBAC** on the engine pod (F2) — a concrete dagmar requirement for ADR-0008.
- **Credential isolation is a Sandbox/encapsulation property**, not a namespace property — sharpens ADR-0007's rationale without changing its storage/scoping decision.

## Gotchas worth recording

- `dl.dagger.io` install script returns **403** from a pod → use the GitHub release tarball for the dagger CLI.
- The Dagger engine image is **Wolfi** (has `dagger`, no `curl`/`wget`/`apk`) → a client container must be alpine-based and add both tools.
- `helm --wait` on a DaemonSet returns before the pod is Ready; `kubectl wait --for=condition=Ready` fails when the pod doesn't exist yet → use `kubectl rollout status daemonset/…`.
- k3s-in-Dagger needs `KeepState: true` + a stable cluster name for image caching.

## Implication for ADR-0008

Hybrid-C in-cluster engine is **feasible and reliable** (kind path; k3s feasible but flaky in nested-Docker). Tenancy = **singleton engine + per-project cache-volume names**; agent pods need SA + `pods/exec` RBAC; namespaces optional. ADR-0008 can be decided on this basis. Production (dedicated runner nodes, ADR-0004) avoids the spike-host nesting/CNI quirks.
