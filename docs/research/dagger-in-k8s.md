# Dagger Engine in Kubernetes: Research Findings

**Research Date:** 2026-07-30
**Dagger Engine Version:** v0.21.8
**Research Branch:** `research/dagger-in-k8s`

## Summary

Dagger provides multiple deployment patterns for Kubernetes, ranging from DaemonSet-based engines to sidecar deployments in CI workflows. The engine runs as a container runtime that executes all Dagger operations as OCI containers. Clients connect to the engine via the `_EXPERIMENTAL_DAGGER_RUNNER_HOST` environment variable, supporting multiple connection protocols (tcp, unix, kube-pod, container, image). Caching is built into the engine with three layers: layer caching (container build steps), volume caching (filesystem directories like npm/maven caches), and function caching (module function results). For the dagmar Hybrid-C architecture, we recommend an in-cluster Dagger engine deployment with a shared cache backend.

## Engine Deployment in Kubernetes

### Official Deployment: Helm Chart (DaemonSet)

Dagger provides an official Helm chart for deploying the engine as a DaemonSet:

```bash
helm upgrade --install --namespace=dagger --create-namespace \
  dagger oci://registry.dagger.io/dagger-helm
```

**Key characteristics:**
- **DaemonSet deployment:** Ensures all matching nodes run an instance of Dagger Engine
- **Architecture goals:**
  - Best utilize local NVMe drives of worker nodes
  - Reduce network latency and bandwidth requirements
  - Simplify routing of Dagger SDK/CLI requests
- **Resource sizing:** Minimum 2 vCPUs and 8GB RAM per runner node recommended

**Architecture pattern (Persistent Nodes):**

```
┌─────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                   │
├─────────────────────────────────────────────────────────┤
│  ┌──────────────┐         ┌──────────────┐             │
│  │ Support Node │         │ Runner Node   │             │
│  │              │         │              │             │
│  │ - Cert Mgr   │         │ - CI Runner  │             │
│  │ - Runner     │<──────> │   (ephemeral)│             │
│  │   Controller │         │ - Dagger     │             │
│  └──────────────┘         │   Engine     │             │
│                           │   (DaemonSet)│             │
│                           │ - Local Cache│             │
│                           └──────────────┘             │
└─────────────────────────────────────────────────────────┘
```

**Source:** [Dagger Kubernetes Deployment Docs](https://docs.dagger.io/reference/deployment/kubernetes)

### Engine Distribution

The Dagger engine (runner) is distributed as a container image:
- **Registry:** `registry.dagger.io/engine`
- **Versioning:** Tagged by engine version (e.g., `v0.12.3`, `v0.21.8`)
- **Image variants:** Standard and GPU-enabled versions

**Source:** [Dagger Custom Runner Docs](https://docs.dagger.io/reference/configuration/custom-runner)

### Alternative: Sidecar Deployment

For CI integrations (Argo, Tekton), Dagger can run as a sidecar container:

```yaml
# Argo Workflows example
sidecars:
  - name: dagger-engine
    image: registry.dagger.io/engine:v0.21.8
    command: ["/usr/local/bin/dagger", "engine"]
    volumeMounts:
      - mountPath: /run/dagger
        name: dagger-engine-socket
```

**Source:** [Dagger Argo Workflows Integration](https://docs.dagger.io/getting-started/ci-integrations/argo-workflows)

## Caching Strategy

### Three Cache Layers

Dagger implements three distinct caching mechanisms:

1. **Layer Caching:** Caches container image build layers. If inputs remain unchanged, layers are automatically reused.

2. **Volume Caching:** Persists specific filesystem directories (e.g., npm, maven, pip cache directories) across runs using cache volumes.

3. **Function Caching:** Persists results of module function calls based on input hashes. Function cache hits skip the entire function body.

**Source:** [Dagger Built-in Caching](https://docs.dagger.io/features/caching), [Dagger Function Caching](https://docs.dagger.io/extending/function-caching)

### Cache Persistence Models

| Mode | Description | Use Case |
|------|-------------|----------|
| `cache="session"` | Results persist only for engine session lifetime | Repeated calls within one workflow |
| `cache="shared"` | Results persist across sessions (requires remote cache) | Shared cache across team/org |
| Built-in volume cache | Filesystem-based cache for dependency managers | npm, maven, pip, etc. |

**Source:** [Dagger Function Caching](https://docs.dagger.io/extending/function-caching)

### Cache Backends

**Local Cache (Default):**
- Stored on engine node's filesystem
- Accessed via DaemonSet local NVMe drives
- Shared among CI runners on same node
- No external configuration required

**Remote Cache (Cloud Engines / Self-hosted):**
- **Dagger Cloud:** Managed shared cache across organization
- **Self-hosted options:**
  - S3-compatible object storage
  - GCS (Google Cloud Storage)
  - Network-attached storage

**Source:** [Dagger Scaling Overview](https://docs.dagger.io/next/adopting/scaling)

## CLI↔Engine Connection Model

### DAGGER_RUNNER_HOST Variable

Clients connect to a Dagger engine using the `_EXPERIMENTAL_DAGGER_RUNNER_HOST` environment variable. Note the `EXPERIMENTAL` prefix - this is currently the de facto standard for production use but not yet officially stabilized.

**Source:** [Dagger GitHub Discussion #10791](https://github.com/dagger/dagger/discussions/10791)

### Connection Protocols

| Protocol | Format | Example |
|----------|--------|---------|
| TCP | `tcp://<address:port>` | `tcp://dagger-engine.default.svc:8080` |
| Unix Socket | `unix://<path>` | `unix:///run/dagger/engine.sock` |
| Kubernetes Pod | `kube-pod://<pod>?namespace=<ns>&container=<cont>` | `kube-pod://dagger-engine-abc?namespace=dagger` |
| Docker Container | `container://<name>` | `container://dagger-engine` |
| Docker Image | `image://<reference>` | `image://registry.dagger.io/engine:latest` |

**Source:** [Dagger Custom Runner Docs](https://docs.dagger.io/reference/configuration/custom-runner)

### Connection Examples

**Kubernetes Pod connection:**
```bash
export _EXPERIMENTAL_DAGGER_RUNNER_HOST="kube-pod://$DAGGER_ENGINE_POD_NAME?namespace=dagger"
dagger call
```

**Unix socket (sidecar):**
```bash
export _EXPERIMENTAL_DAGGER_RUNNER_HOST="unix:///run/dagger/engine.sock"
dagger call
```

**Source:** [Dagger K8s Reference Docs](https://docs.dagger.io/reference/deployment/kubernetes)

### Connection Security

Dagger does not implement encryption for data sent "over the wire." It relies on the underlying connection type:
- **Unix sockets:** Secure (local filesystem)
- **TCP:** Requires TLS/network-level security
- **kube-pod:** Relies on Kubernetes network policies

**Source:** [Dagger Custom Runner Docs](https://docs.dagger.io/reference/configuration/custom-runner)

## Execution Model: Hermetic Module Containers

### Dagger-in-Dagger (DinD)

Dagger supports running the engine inside a container (Docker-in-Docker model):
- The engine runs as a containerized container runtime
- All operations execute as nested OCI containers
- Requires `--privileged` flag for nested container execution
- GPU support available with `--gpus all` and `_EXPERIMENTAL_DAGGER_GPU_SUPPORT`

**Source:** [Dagger GitHub Discussion #5026](https://github.com/dagger/dagger/discussions/5026)

### Module Isolation

Dagger modules execute in isolated containers with:
- **Typed artifacts:** Content-addressed, can cross SDK/language boundaries
- **Hermetic builds:** All dependencies explicit and strictly typed
- **No host dependency leakage:** Tools run in containers, orchestrated by sandboxed functions

**Source:** [Dagger GitHub Repository](https://github.com/dagger/dagger)

### GPU Support

For GPU workloads:
```bash
docker run --gpus all -d --privileged \
  -e _EXPERIMENTAL_DAGGER_GPU_SUPPORT=true \
  --name dagger-engine-${VERSION} \
  registry.dagger.io/engine:${VERSION}-gpu
```

**Source:** [Dagger Custom Runner Docs](https://docs.dagger.io/reference/configuration/custom-runner)

## Hybrid-C Wiring Recommendation for Dagmar

Based on this research, the recommended architecture for dagmar's Hybrid-C (Go/K8s control plane + Dagger engine) is:

### Architecture Diagram

```
┌────────────────────────────────────────────────────────────────┐
│                      Kubernetes Cluster                        │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Go Control Plane (dagmar-core)             │   │
│  │  - Agent orchestration                                 │   │
│  │  - Task scheduling                                     │   │
│  │  - K8s Job/Workflow management                          │   │
│  └────────────┬──────────────────────────────────────────┘   │
│               │                                                  │
│               │ DAGGER_RUNNER_HOST=kube-pod://...              │
│               ▼                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │              Dagger Engine (DaemonSet)                   │   │
│  │  - registry.dagger.io/engine:v0.21.8                    │   │
│  │  - Shared cache (PV or remote backend)                  │   │
│  │  - Privileged mode (for nested containers)              │   │
│  └────────────┬──────────────────────────────────────────┘   │
│               │                                                  │
│               │ Module containers (executed by engine)          │
│               ▼                                                  │
│  ┌─────────────────────────────────────────────────────────┐   │
│  │           Agent Pods (ephemeral)                        │   │
│  │  - Dagger CLI / SDK client                              │   │
│  │  - Agent logic (LLM, tools)                             │   │
│  │  - Hermetic module execution                           │   │
│  └─────────────────────────────────────────────────────────┘   │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

### Key Design Decisions

**1. In-Cluster Engine (DaemonSet)**
- Deploy Dagger engine as DaemonSet on dedicated runner nodes
- Enables cache sharing across agent pods on same node
- Reduces network overhead for cache operations
- Simplifies connection via `kube-pod://` protocol

**2. Agent Pods as Clients**
- Agent pods are lightweight clients containing Dagger CLI/SDK
- Connect to engine via `_EXPERIMENTAL_DAGGER_RUNNER_HOST`
- Execute Dagger modules hermetically within engine

**3. Shared Cache Strategy**
- **Primary:** Local persistent volume attached to runner nodes (fast)
- **Secondary (optional):** Remote cache backend (S3/GCS) for cross-node sharing
- Cache hierarchy: Node-local (fastest) → Remote (fallback)

**4. Connection Configuration**
```yaml
# Agent pod environment
env:
  - name: _EXPERIMENTAL_DAGGER_RUNNER_HOST
    value: "kube-pod://dagger-engine-xxxx?namespace=dagmar"
  - name: DAGGER_CACHE
    value: "type=registry,ref=dagmar-cache:latest"  # Optional remote cache
```

**5. Isolation & Hermeticity**
- Each module execution runs in isolated container
- No direct host access from modules
- All dependencies explicit in module code
- Secrets handled via Dagger secret providers (not filesystem)

### Deployment Steps

1. **Deploy Dagger Engine DaemonSet:**
```bash
helm upgrade --install --namespace=dagmar --create-namespace \
  dagger-engine oci://registry.dagger.io/dagger-helm \
  --set image.tag=v0.21.8 \
  --set privileged=true
```

2. **Configure Agent Pods:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dagmar-agent
spec:
  containers:
    - name: agent
      image: dagmar-agent:latest
      env:
        - name: _EXPERIMENTAL_DAGGER_RUNNER_HOST
          value: "kube-pod://dagger-engine-xxxx?namespace=dagmar"
```

3. **Optional: Configure Remote Cache:**
```yaml
# For shared cache across nodes
env:
  - name: DAGGER_CACHE
    value: "s3://dagmar-cache?region=us-east-1"
```

### Benefits

- **Cache efficiency:** DaemonSet placement maximizes cache reuse
- **Scalability:** Add runner nodes = add engine instances + cache
- **Hermeticity:** Module isolation preserved through engine
- **Flexibility:** Can switch between local and remote cache
- **Operational simplicity:** Single DaemonSet manages engines

## Open Questions

1. **Cache Poisoning Risk:** How to secure shared cache when untrusted code runs? May need per-tenant cache isolation.

2. **Engine Version Management:** The `_EXPERIMENTAL_DAGGER_RUNNER_HOST` protocol requires matching CLI/engine versions. How to handle upgrades?

3. **Multi-Tenancy:** Can a single engine serve multiple isolated agents safely, or do we need per-namespace engines?

4. **GPU Support:** For AI/ML workloads, how to provision GPU-enabled engines with appropriate scheduling?

5. **Protocol Stability:** `_EXPERIMENTAL_DAGGER_RUNNER_HOST` is still experimental. What is the timeline for stabilization?

6. **Remote Cache Backend:** Dagger Cloud uses managed cache. For self-hosted, what's the recommended S3/GCS integration pattern?

## Sources

- [Dagger Kubernetes Deployment (v0.21.7)](https://docs.dagger.io/0.21.7/adopting/scaling/kubernetes)
- [Dagger Kubernetes Deployment Reference](https://docs.dagger.io/reference/deployment/kubernetes)
- [Dagger Custom Runner Configuration](https://docs.dagger.io/reference/configuration/custom-runner)
- [Dagger Built-in Caching](https://docs.dagger.io/features/caching)
- [Dagger Function Caching](https://docs.dagger.io/extending/function-caching)
- [Dagger Scaling Overview](https://docs.dagger.io/next/adopting/scaling)
- [Dagger Argo Workflows Integration](https://docs.dagger.io/getting-started/ci-integrations/argo-workflows)
- [Dagger GitHub Repository](https://github.com/dagger/dagger)
- [Stabilize the Engine Remote Protocol Discussion #10791](https://github.com/dagger/dagger/discussions/10791)
- [Dagger-in-Docker Discussion #5026](https://github.com/dagger/dagger/discussions/5026)
