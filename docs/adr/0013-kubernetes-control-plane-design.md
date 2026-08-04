# ADR-0013: Kubernetes control-plane design — topology, reconciliation & dispatch

Date: 2026-08-04
Seed: dagmar-67bc (part of dagmar-80dd) · Status: **PROPOSED**
Decided via grilling 2026-08-04. Resolves the wayfinder "Kubernetes control-plane design
(CRD/operator vs. controller+workers; dispatch; concurrency)" item and is the design home for
the control-plane internals ADR-0012 explicitly deferred out of Phase 0.

## Context

`dagmar-67bc` asks: what is dagmar's Kubernetes control-plane design — controller topology,
reconciliation model, and dispatch/concurrency semantics — that the Phase-0 lean controller
(ADR-0012 §3, the `RunReconciler`) grows into? Phase 0 proved the dispatch vertical (CRD →
controller → agent pod → `kube-pod://` → engine, commit d549007) and the gate-family (Phase 1,
commit ca26df7). This ADR fixes the design that vertical grows toward.

**Already decided (do not re-litigate):**

- CRD set: {Project, Agent, Prompt, QualityGate, Trigger, Run} (ADR-0002).
- Hybrid-C topology: K8s control-plane + in-cluster singleton Dagger engine (ADR-0004).
- Agent pods are `kube-pod://` clients of the singleton engine; per-Project SA + `pods/exec` +
  `pods:get` RBAC + distinct cache-vol names (ADR-0008). The controller provisions these.
- Root module hosts the CRD types (`api/v1alpha1/`) + controller (`cmd/dagmar-controller/`);
- Merge is a deterministic controller function, never an LLM action; the merge tool is in no
  Agent's tool-set (ADR-0006).
- os-eco is read-local inside the hermetic loop; sync is networked outside it (ADR-0011 §9).

**Out of scope (separate seeds):** the event/trigger model (GitHub webhooks, manual,
subscription) — that is Phase 3 autonomy (ADR-0012 §5). This ADR fixes the *dispatch/reconcile
core*, not event sources.

## Decision

### 1. Topology — one manager, one controller per active CRD; no worker pool (D1, D2)

One `controller-runtime` manager process runs all controllers; each **active** CRD gets its own
controller (`Run` now; `Project`/`Agent` reconcile as configuration that provisions identity +
pods). There is **no separate worker process**: the agent pods *are* the execution units
(hybrid-C — the control plane owns state/reconcile, the engine+pods own execution). A
controller+worker split would duplicate the execution layer the agent pods already provide.

**Admission webhooks: none this increment.** The controller validates reconcile-side and fails
terminally on invalid input (review-12: invalid `moduleRef`/`moduleFunction` → `Failed`). A
validating webhook is a real win only for pre-accept reject (quota, ref resolution before store)
or defaulting — revisit deliberately then, not speculatively now. This keeps the "lean,
growing" posture (ADR-0012 §3).

### 2. Reconciliation model — Pod-watch requeue, identity finalizer, metav1.Condition (D3–D5)

**Requeue (D3):** the controller creates the agent pod with an `OwnerReference` to the owning
`Run` and watches `Pod` events filtered to that owner. A pod transition (Running / Succeeded /
Failed) requeues the `Run` automatically — no blind polling. Coarse progress (`Dispatched` when
the pod is `Running`) needs no extra RBAC; the pod never writes `Run` status back (least
privilege, ADR-0008).

**Finalizer / cleanup (D4):** a finalizer on `Project` tears down the provisioned identity (SA +
`RoleBinding`s). The per-Project **cache-vol is retained** (warm — fast re-create; dagmar's
value is iteration speed). A reclaim policy (TTL / size) is a later increment. `Run` deletion
garbage-collects its pod via the owner reference.

**Status conditions (D5):** standard `metav1.Condition` with a pinned minimal set, grown
deliberately: `Run` {Accepted, Progressing, Succeeded, Failed}; `Project` {Provisioned, Ready}.
Idiomatic, k9s-readable, and carries reason/message/lastTransition.

### 3. Dispatch concurrency — per-Project serialized, cross-Project parallel (D6, D7)

**Concurrency granularity (D6):** within a `Project`, at most **one dispatching `Run`** at a
time — the workspace (the repo checkout on the cache-vol) is shared mutable state; two agents
editing one checkout corrupt it. Different `Project`s run concurrently (isolated cache-vols +
namespaces, ADR-0008; the singleton engine serves many). Global serialization would waste engine
capacity; unlimited concurrency would race the working tree.

**Workspace-lineage pointer (D7):** controller-owned, stored on `Project.Status` as
`lineageHead` (commit SHA) + `activeRun`. The controller sets `activeRun` on dispatch, clears it
on terminal, and advances `lineageHead` to the run's output commit. A new `Run` for that
`Project` requeues while `activeRun` is set (Run-out → next Run-in). Living on the CR the
controller already reconciles avoids a pod-owned linked-list walk (racy under concurrent
reconcile) and a separate `coordination.k8s.io` Lease (unneeded when the controller centrally
sequences).

### 4. Provisioning fields (D8–D10)

- **`moduleRef` (D8):** stays on `Project.spec` — it is the project's capability module (the
  Dagger module whose functions the agent calls). The Phase-0 home is confirmed as the design
  home.
- **`agent-pod-image` (D9):** a **platform default** (a `dagmar-config` ConfigMap / controller
  flag), versioned with dagmar's release, optionally overridable on `Agent.spec`. The harness
  image is platform infra, not project-specific; one image suffices now, and the `Agent` CR stays
  cognitive (model + tool-set + prompt), not a pod template.
- **`engine-git-creds` (D10):** per-Project **scoped** credentials — `Project.spec.gitCredentialsRef`
  → a `Secret` in the `Project`'s namespace (deploy token / fine-grained PAT, `contents: read`
  on the module repo only). The singleton engine holds **no broad creds**; each dispatch carries
  the scoped credential for that `Project`'s module. This matches per-Project namespace isolation
  (ADR-0008) and the two-layer credential defense (ADR-0007 / A1). Dogfood: `dagmar`-as-a-Project
  carries `dagmar-git-creds` to read its own (now private) module.

  > **Spike-bound (mechanism):** exactly how the singleton engine receives per-dispatch scoped
  > git creds under Dagger v0.21.x is implementation-uncertain and is resolved by a small spike
  > (prototypes-for-decisions), not asserted here. The **shape** (per-Project scoped, engine
  > holds none) is decided.

### 5. State & persistence — hybrid CRs + object-storage; os-eco as manifest-declared tool-ports (D11, D12)

**State home (D11):** **hybrid**. Structured `Run`/`Project` status lives on the CRs (k9s-queryable,
Kubernetes-native); large unstructured blobs (logs, outputs, traces) live in object storage
(MinIO/S3), referenced via `Status.artifactRef`. A retention policy prunes completed `Run`s to
bound etcd. Early increments may start CR-only and add object storage when logs outgrow CRs;
object storage is the long-term home for blobs regardless.

**os-eco bindings (D12):** seeds (issues) and mulch (expertise) are exposed to the LLM through
**abstract, manifest-declared interfaces**, not hard-coded. The `ProjectManifest`
(`.dagmar/project.yaml`) declares each binding as bash commands with placeholders; dagmar wraps
each into a dedicated LLM tool (e.g. `issues_read`, `issues_write`, `expertise_read`,
`expertise_write`). This is the hexagonal port/adapter pattern (ADR-0010) applied to os-eco, and
the same separation as checkables/gate (manifest = *what*, tool-wrapper = *how*) — making dagmar
**vendor-agnostic** (swap the tracker or expertise store by changing the commands, not the
tools' signatures).

Hermeticity (ADR-0011 §9) is preserved by five rules:

1. **Read-only is a contract *and* sandbox-enforced** — the manifest classifies a command's
   read/write intent, but the in-loop sandbox independently guarantees hermeticity
   (network-restricted, FS read-only except the workspace). A misclassified "read" cannot escape.
2. **Placeholders → argv, never string interpolation** — the tool wrapper passes LLM-supplied
   args as separate argv elements (or strictly escaped), never raw into a shell string. This
   removes shell-injection as an attack surface.
3. **In-loop reads are read-local (network-free)** — mulch and seeds are in-repo git-native, so
   read-local is hermetic. A binding whose read needs the network (a live remote tracker) is
   out-of-loop only.
4. **Writes execute in a separate, networked context — never in the loop** — write tools are
   given to the LLM only in a non-hermetic turn (a post-run record step / a gated external
   agent). The loop never mutates os-eco. This is ADR-0011 §9 realized as a tool-availability
   rule.
5. **Home is the manifest, not the `Project` CR** — the commands are project-specific and
   version with the repo; the `Project` CR carries the pointer + credentials + runtime overrides.

  > **Spike-bound (mechanism):** how dagmar turns manifest-declared bash commands into real LLM
  > tools inside the Dagger LLM primitive depends on the primitive's tool-registration surface
  > (Phase 2 cognition). The **design** (abstract interface, manifest-declared, read-in-loop /
  > write-gated) is decided; the wrapping mechanism is spike-bound.

## Alternatives considered

- **Controller + worker pool (D1):** rejected — duplicates the execution layer the agent pods
  already are.
- **One controller per CRD as separate binaries (D1):** rejected — operational overhead with no
  benefit at six CRDs and one team.
- **Admission webhooks now (D2):** deferred — the win (pre-accept reject / defaulting) does not
  yet justify the moving parts; reconcile-side validation suffices.
- **Poll-based requeue (D3):** rejected — wastes reconciles and delays terminal detection by up
  to one interval; the owner-ref watch is strictly better.
- **Pod writes `Run` status back (D3):** rejected — needs write RBAC on `Run`s for the pod SA,
  breaking least privilege.
- **Full teardown incl. cache-vol on `Project` delete (D4):** rejected — loses the warm cache,
  against dagmar's iteration-speed value; retain + later reclaim policy instead.
- **`Status.Phase` string (D5):** rejected — loses reason/message/lastTransition; `metav1.Condition`
  is idiomatic and richer for equal cost.
- **Global Run serialization (D6):** rejected — wastes singleton-engine capacity across `Project`s.
- **Lineage as a linked list on `Run`s / a `coordination.k8s.io` Lease (D7):** rejected — the
  list walk is racy under concurrent reconcile; the Lease adds an object + renewal for something
  the controller centrally sequences already.
- **`agent-pod-image` on `Project.spec` or fixed on `Agent.spec` (D9):** rejected — the former
  conflates project conformance with platform infra (N-fold maintenance); the latter forces every
  `Agent` to know a platform image and conflates persona with pod template.
- **Platform engine-wide git creds (D10):** rejected — the singleton engine holding broad read
  access across all `Project`s is a blast-radius violation of the isolation principle.
- **CR-only or external-DB state (D11):** CR-only bloats etcd once logs accumulate; an external DB
  is a new stateful component unjustified at this scale and breaks the Kubernetes-native line.
- **Read-only snapshot mount for mulch (D12):** superseded — the manifest-declared tool-port is
  strictly more general (vendor-agnostic, data-driven tool-set) and still hermetic.

## Consequences

- The root controller grows one controller per active CRD behind one manager; `RunReconciler`
  gains an owner-ref pod watch + `metav1.Condition` writes. `Project` gains a finalizer (identity
  teardown) + `Status {lineageHead, activeRun}`.
- Concurrency is bounded naturally: one dispatching `Run` per `Project`, unbounded across
  `Project`s. No lock object is introduced.
- Private module repos work via per-Project scoped secrets once the engine-git-creds **mechanism
  spike** confirms the v0.21.x path; dagmar's own private repo is the first dogfood case (and the
  pressure that made this decision load-bearing).
- The `ProjectManifest` grows an `os-eco` section (issues + expertise bindings as commands); the
  LLM tool-set becomes partially manifest-derived. Realized in Phase 2 cognition after the
  os-eco-tool **wrapping spike**.
- Two spikes are filed out of this ADR: (a) engine-git-creds mechanism; (b) os-eco tool-wrapping
  mechanism. Both are "design fixed, mechanism open."

## Deferred

- **Event & trigger model** (Phase 3 autonomy, ADR-0012 §5) — a separate seed.
- **The engine-git-creds mechanism spike** and the **os-eco tool-wrapping mechanism spike** —
  resolved by prototype, then folded back as a note here.
- **Cache-vol reclaim policy** (TTL / size) — a later increment once retention needs are concrete.
- **Object-storage backend selection + `artifactRef` schema** — decided when the first increment
  outgrows CR-only state.
