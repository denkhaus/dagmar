# ADR-0013: Kubernetes control-plane design — topology, reconciliation & dispatch

Date: 2026-08-04
Seed: dagmar-67bc (part of dagmar-80dd) · Status: **ACCEPTED**
Decided via grilling 2026-08-04; revised after dagmar-review 15
(`docs/review/15-2026-08-04-2b35fad-67bc.md`, NEEDS-WORK → revised: FIX-1 hermeticity model,
GAP-1 Task-granularity, GAP-2/3 engine-git-creds honesty, GAP-4 os-eco feasibility, GAP-5
stale-pointer recovery, HOUSE/SPEC sharpening; then accepted). D10's engine-git-creds spike
(`dagmar-2c68`) was resolved 2026-08-05: the `#8805` client-credential mechanism confirmed, the
no-standing-engine-cred invariant holds, ADR-0007 consistency lifted from conditional to settled.
Resolves the wayfinder
"Kubernetes control-plane design (CRD/operator vs. controller+workers; dispatch; concurrency)"
item and is the design home for the control-plane internals ADR-0012 deferred out of Phase 0.

## Context

`dagmar-67bc` asks: what is dagmar's Kubernetes control-plane design — controller topology,
reconciliation model, and dispatch/concurrency semantics — that the Phase-0 lean controller
(ADR-0012 §3, the `RunReconciler`) grows into? Phase 0 proved the dispatch vertical (CRD →
controller → agent pod → `kube-pod://` → engine, commit d549007) and the gate-family (Phase 1,
commit ca26df7). This ADR fixes the design that vertical grows toward.

**Already decided (do not re-litigate):**

- CRD set: {Project, Agent, Prompt, QualityGate, Trigger, Run} (ADR-0002). Non-CR: Task (≡ one
  seeds issue), Sandbox, Workspace, ProjectManifest.
- Hybrid-C topology: K8s control-plane + in-cluster singleton Dagger engine (ADR-0004).
- Agent pods are `kube-pod://` clients of the singleton engine; per-Project SA + `pods/exec` +
  `pods:get` RBAC + distinct cache-vol names (ADR-0008). The controller provisions these.
- Root module hosts the CRD types (`api/v1alpha1/`) + controller (`cmd/dagmar-controller/`).
- Merge is a deterministic controller function, never an LLM action; the merge tool is in no
  Agent's tool-set (ADR-0006).
- os-eco is read-local inside the hermetic loop; sync is networked outside it (ADR-0011 §9).
- **Workspace is per-Run isolated (no shared clone); Workspace lineage is per-Task (CONTEXT.md).**
  Load-bearing for §3: there is no shared mutable checkout between Runs, so concurrency does not
  race a workspace — the serialization driver is lineage dependency within a Task.

**Out of scope (separate seeds):** the event/trigger model (GitHub webhooks, manual,
subscription) — Phase 3 autonomy (ADR-0012 §5). This ADR fixes the dispatch/reconcile core.

## Decision

### 1. Topology — one manager, one controller per active CRD; no worker pool (D1, D2)

One `controller-runtime` manager process runs all controllers; each active CRD gets its own
controller (`Run` now; `Project`/`Agent` reconcile as configuration that provisions identity +
pods). There is no separate worker process: the agent pods are the execution units (hybrid-C —
the control plane owns state/reconcile, the engine+pods own execution). A controller+worker
split would duplicate the execution layer the agent pods already provide.

Admission webhooks: none this increment. The controller validates reconcile-side and fails
terminally on invalid input (review-12: invalid `moduleRef`/`moduleFunction` → `Failed`). A
validating webhook is a real win only for pre-accept reject (quota, ref resolution before store)
or defaulting — revisit deliberately then. Keeps the "lean, growing" posture (ADR-0012 §3).

### 2. Reconciliation model — Pod-watch requeue, identity finalizer, metav1.Condition (D3–D5)

**Requeue (D3):** the controller creates the agent pod with an `OwnerReference` to the owning
`Run` and watches `Pod` events filtered to that owner. A pod transition (Running / Succeeded /
Failed) requeues the `Run` automatically — no blind polling. Coarse progress (`Dispatched` when
the pod is `Running`) needs no extra RBAC; the pod never writes `Run` status back (least
privilege, ADR-0008).

**Finalizer / cleanup (D4):** a finalizer on `Project` tears down the provisioned identity (SA +
`RoleBinding`s). The per-Project cache-vol is retained (warm — fast re-create; dagmar's value is
iteration speed). The retained cache-vol is an engine-side object (cache identity keys by volume
name, ADR-0008 §3), NOT a namespace object — the namespace-scoped finalizer does not touch it, so
warm retention leaves an engine-side artifact outside the `Project` namespace with no named owner
until a reclaim policy exists. Reclaim ownership (controller / engine / ops) and policy (TTL /
size) are a later increment (review-15 HOUSE-4). `Run` deletion garbage-collects its pod via the
owner reference.

> **Implementation reality (2026-08-06) — D4 deferred.** The Project-level identity finalizer is
> **blocked** on identity being safely per-Run-deletable, which it is not yet. Today the controller
> provisions identity per-Run but **shared-named** within a namespace (SA `dagmar-agent`; Role +
> RoleBinding `dagmar-agent-exec` in the engine namespace — the latter is cross-namespace, so NOT
> owner-ref'd to the Run, a documented leak carried over from dagmar-67bc). A Run- or Project-level
> finalizer that deleted these shared-named resources would **break sibling Runs** in the same
> namespace — and this break is **not hypothetical**: the shared-named SA is itself controller-owner-ref'd
> to its *creating* Run (`ensureAgentIdentity`), so deleting that Run **already cascades via k8s GC
> today** — the SA vanishes and every sibling Run's pods (which run as that SA) lose their identity
> (admission failures, broken exec-into-engine). No finalizer is required for the harm; the identity
> refactor must also drop or re-scope that SA owner-ref. Per-Project namespaces are not provisioned
> either (Runs run in the Project's namespace, `default`), and there is no `ProjectReconciler`. So
> neither a Run- nor Project-level finalizer can cleanly tear down identity today. D4 is unblocked by
> the identity-refactor seed (make identity per-Run-named, OR provision per-Project namespaces + a
> `ProjectReconciler` — either way also dropping/re-scoping the SA controller owner-ref so its GC no
> longer cascades); until then the engine-namespace Role/RoleBinding leak + the SA-owner-ref GC
> sibling-break remain the documented interim gaps. (Mirrors the ADR-0014 GAP-1 interim-note honesty
> pattern: the divergence is on record, not silent.)

**Status conditions (D5):** standard `metav1.Condition` with a pinned minimal set, grown
deliberately: `Run` {Accepted, Progressing, Succeeded, Failed}; `Project` {Provisioned, Ready}.
Idiomatic, k9s-readable, carries reason/message/lastTransition. `Status.Phase` is RETAINED as a
derived back-compat summary (`phaseFromConditions`); the conditions are authoritative — a rejection
and a pod-failure both surface `Phase=Failed` but differ on `Accepted` (False vs True).

### 3. Dispatch concurrency & lineage — per-Task lineage serialization; parallel across Tasks/Projects (D6, D7)

> **Review-15 GAP-1 revision.** An earlier draft serialized at **Project** granularity on a
> "shared mutable workspace" premise. That premise is false: CONTEXT.md defines `Workspace` as
> per-Run isolated ("no shared clone — avoids file-change collisions"). The real serialization
> driver is **lineage dependency within a Task**. Granularity is corrected to Task.

**Concurrency granularity (D6):** within a Task, at most one dispatching Run at a time — but the
reason is **lineage dependency, not a shared-checkout race**: Run N's base ref is Run N−1's
output commit (CONTEXT.md: "Workspace lineage across a Task's Runs, Run-out → next Run-in"), so
Run N cannot start until Run N−1 produces its output. A "dispatching Run" = a Run past `Accepted`
with an active pod (`Progressing`/`Dispatched`), equivalently the Task's single non-terminal Run
(review-15 HOUSE-5). Across Tasks (same Project) and across Projects, Runs execute in parallel —
Workspaces are per-Run isolated, so there is no file collision to prevent. Cross-Task and
cross-Project parallelism is safe contingent on the controller's allocation invariants from
ADR-0008 §3 — distinct cache-vol names and distinct namespaces per Project — which the
provisioning design (§4) must preserve (review-15 HOUSE-2).

This answers ADR-0008 §5's deferred question ("whether concurrent Runs on one Task are allowed"):
within a Task, **no** (lineage-ordered); across Tasks and Projects, **yes**.

**Lineage pointer (D7):** lineage is per-Task and tracked on the `Run` CRs that share a
`spec.taskRef` (Task ≡ one seeds issue, a non-CR reference). Each Run carries
`status.outputCommit` and `status.predecessorRun`. The controller derives a Task's state from Run
status rather than storing a single pointer: the active Run = the non-terminal Run with that
`taskRef`; the lineage head = the latest terminal Run's `outputCommit`. A new Run for a Task
requeues while a non-terminal Run for that `taskRef` exists; on dispatch it uses the lineage
head's `outputCommit` as its base ref. There is no single field on `Project` — a Project has N
Tasks, each an independent lineage.

**Stale-pointer / crash recovery (review-15 GAP-5):** the active-Run gate is "a non-terminal Run
with this `taskRef` exists." If, on reconcile, that Run's pod is absent (`NotFound`) without a
terminal Condition — node failure, force-delete, eviction — the controller marks the Run terminal
(`Failed`, reason `AgentPodLost`), which frees the Task for its next Run. A Run holds its Task's
slot until it reaches a terminal Condition OR its pod is observed absent, whichever the
controller sees first.

### 4. Provisioning fields (D8–D10)

- **`moduleRef` (D8):** stays on `Project.spec` — the project's capability module (the Dagger
  module whose functions the agent calls). Phase-0 home confirmed as the design home.
- **`agent-pod-image` (D9):** a platform default (a `dagmar-config` ConfigMap / controller flag),
  versioned with dagmar's release, optionally overridable on `Agent.spec`. The Alternatives
  reject making the image **mandatory/fixed** on `Agent.spec` (conflates persona with pod
  template, forces every Agent to know a platform image). An **optional override** is different:
  the default covers the common case and the override is an escape hatch for the rare persona
  that genuinely needs a different harness — it does not re-conflate, because the platform still
  owns the default (review-15 HOUSE-3).
- **`engine-git-creds` (D10) — RESOLVED (spike `dagmar-2c68`, 2026-08-05):** per-Project scoped
  credentials whose **source, projection, delivery, and invariant** are confirmed against primary
  sources (Dagger PR #8805 + v0.14.0 release notes; v0.21.8 generated API types; the
  remote-repositories doc). The earlier "spike-gated / conditional on ADR-0007" caveat is lifted.
  - **Source (unchanged):** `Project.spec.gitCredentialsRef` → a `Secret` in the `Project`'s
    namespace (deploy token / fine-grained PAT, `contents: read` on the module repo only). Dogfood:
    `dagmar`-as-a-Project carries `dagmar-git-creds` to read its own now-private module.
  - **Projection:** the controller projects the `Secret` into the agent pod as an env var and
    configures a headless `git` credential helper in the pod's gitconfig that emits that PAT
    (non-interactive — no credential-manager GUI).
  - **Delivery (the corrected mechanism):** when the agent pod runs `dagger call -m <moduleRef>`,
    the engine queries the **client pod's** `git credential fill` for the repo's host, receives the
    PAT, and **injects it back into that session as a Dagger secret** (PR #8805, "PAT support —
    private git on http/https", shipped v0.14.0, present in v0.21.8). The engine then fetches the
    private module source with that session-scoped secret. This is module-source loading (Path A)
    — distinct from in-function `dag.Git(url, HTTPAuthToken: *Secret)` (Path B), which does **not**
    apply to `-m` module loading.
  - **Invariant holds — GAP-2/GAP-3 resolved FAVORABLY:** the engine holds **no standing
    credential**. The PAT is resolved client-side, lives only within the one session that requested
    it, is never persisted by the engine, and Dagger secrets are isolated across client sessions
    (per-session/per-client scope). Each Project's agent pod brings its own PAT, so there is no
    cross-Project leakage. This composes cleanly with ADR-0007/A1's per-Project namespace boundary:
    the cred originates in one Project's namespace `Secret`, is projected into that Project's pod,
    and is consumed transiently by the engine within that pod's session.
  - **Implementation TODO (not a design decision):** the exact headless credential-helper wiring in
    the agent-pod image (env var → `credential.helper '!f() { …; }'`, or a netrc) is settled in the
    dispatch-vertical implementation increment, not here.

### 5. State & persistence — hybrid CRs + object-storage; os-eco as manifest-declared tool-ports (D11, D12)

> **D12 partially superseded by ADR-0017:** the manifest-declared bash-command os-eco binding
> mechanism described in this section (the five hermeticity rules, `issues_read`/`issues_write`
> tool names, feasibility gate on the spike) is replaced by ADR-0017's named-function approach
> (Project Hooks as Dagger module functions). D11 (state home) is unaffected.

**State home (D11):** hybrid. Structured Run/Project status on the CRs (k9s-queryable,
Kubernetes-native); large unstructured blobs (logs, outputs, traces) in object storage
(MinIO/S3), referenced via `Status.artifactRef`. A retention policy prunes completed Runs to
bound etcd. Early increments may start CR-only and add object storage when logs outgrow CRs;
object storage is the long-term home for blobs regardless.

**os-eco bindings (D12):** seeds (issues) and mulch (expertise) are exposed to the LLM through
abstract, manifest-declared interfaces, not hard-coded. The `ProjectManifest` declares each
binding as bash commands with placeholders; dagmar wraps each into a dedicated LLM tool
(`issues_read`, `issues_write`, `expertise_read`, `expertise_write`). Hexagonal port/adapter
(ADR-0010) applied to os-eco; manifest = *what*, tool-wrapper = *how* — vendor-agnostic.

**Feasibility assumption, stated explicitly (review-15 GAP-4):** D12 requires the Dagger LLM
primitive to expose a **data-driven tool-registration surface** (tools derivable from
manifest-declared commands, per Project). If the primitive's tool surface is static (tools fixed
at `Env` construction), D12 is not implementable without upstream changes — so primitive
feasibility (can manifest bash commands become real LLM tools at all?) is spike `dagmar-e8f3`
question #1, ahead of the hermeticity/argv mechanics.

Hermeticity (ADR-0011) is preserved by five rules:

1. **Hermeticity is a tool-surface constraint, not a network air-gap (review-15 FIX-1).** The
   singleton engine's container exec has outbound network by default (ProbeNet), and there is no
   per-exec no-network lever in Dagger v0.21.x (`ContainerWithExecOpts` has no network field) —
   this residual is deliberately accepted (ADR-0011 §3). The agent therefore reaches os-eco ONLY
   through the curated in-loop tools, because the raw `container`/`git`/`http` tools are withheld
   from hermetic agents (ADR-0011 §2) — there is no alternate path to read or mutate the stores.
   The manifest's read-local-vs-network classification is consequently a **load-bearing,
   reviewable invariant**: only read-local commands are admitted to the in-loop tool-set; a
   networked read is out-of-loop only. (An earlier draft's "network-restricted sandbox" and "FS
   read-only except workspace" claims are **retracted** — no mechanism establishes either; the
   control is tool-set curation, and defense-in-depth — any escape bounded to one Project's
   namespace + projection — is ADR-0007's, restated here, not invented.)
2. **Placeholders → argv substantially reduces shell-injection surface (review-15 HOUSE-1)** when
   the wrapper argv-applies the placeholders and the manifest body does not re-interpolate them.
   Because the bindings are bash bodies invoked through a shell, this is a real reduction of
   surface, not its elimination — and enforcing the argv/escape discipline is a property the
   wrapping spike (`dagmar-e8f3`) must guarantee.
3. **In-loop reads are read-local (network-free)** — mulch and seeds are in-repo git-native, so
   read-local is hermetic. After FIX-1, **this rule carries hermeticity** (not Rule 1): a binding
   whose read needs the network is out-of-loop only, and that classification is the primary
   control the wrapper enforces.
4. **Writes execute in a separate, networked context — never in the loop** — write tools are
   given to the LLM only in a non-hermetic turn (a post-run record step / a gated external
   agent). The loop never mutates os-eco (ADR-0011 §9 as a tool-availability rule).
5. **Home is the manifest, not the `Project` CR** — the commands are project-specific and version
   with the repo; the `Project` CR carries the pointer + credentials + runtime overrides.

## Alternatives considered

- **Controller + worker pool (D1):** rejected — duplicates the execution layer the agent pods are.
- **One controller per CRD as separate binaries (D1):** rejected — operational overhead, no benefit.
- **Admission webhooks now (D2):** deferred — pre-accept reject / defaulting does not yet justify the moving parts.
- **Poll-based requeue (D3):** rejected — wastes reconciles, delays terminal detection; owner-ref watch is strictly better.
- **Pod writes `Run` status back (D3):** rejected — needs write RBAC on `Run`s for the pod SA, breaking least privilege.
- **Full teardown incl. cache-vol on `Project` delete (D4):** rejected — loses the warm cache.
- **`Status.Phase` string (D5):** rejected — loses reason/message/lastTransition.
- **Project-granularity serialization (D6, review-15 GAP-1):** rejected — too strict (blocks concurrent Tasks within a Project, which the per-Run-isolated Workspace model permits) and justified by a shared-checkout premise the docs deny. Task granularity matches CONTEXT.md + ADR-0008 §5.
- **Linked-list / Lease for lineage (D7):** a Run-status-derived per-Task lineage avoids both a racy cross-reconcile walk and an extra `coordination.k8s.io` Lease; the controller already reconciles Runs.
- **`agent-pod-image` on `Project.spec` / fixed on `Agent.spec` (D9):** rejected — the former conflates project conformance with platform infra; the latter forces every Agent to know a platform image and conflates persona with pod template. Optional override on `Agent.spec` is the chosen middle.
- **Platform engine-wide git creds (D10, review-15 GAP-2):** rejected — the singleton engine holding broad read access across all Projects is a blast-radius violation of the isolation principle. The chosen per-Project scoped shape is the right direction, with ADR-0007 consistency conditioned on the spike.
- **CR-only or external-DB state (D11):** CR-only bloats etcd once logs accumulate; an external DB is a new stateful component unjustified at this scale.
- **Read-only snapshot mount for mulch (D12):** superseded — the manifest-declared tool-port is strictly more general and still hermetic.

## Consequences

- The root controller grows one controller per active CRD behind one manager; `RunReconciler`
  gains an owner-ref pod watch + `metav1.Condition` writes + a per-Task lineage gate derived from
  Runs sharing `spec.taskRef`. `Project` gains a finalizer (identity teardown).
- Concurrency: one dispatching Run per Task (lineage-ordered), parallel across Tasks and
  Projects. No lock object introduced; lineage is derived from Run status. A lost pod is detected
  on reconcile and frees the Task (GAP-5).
- Private module repos work via the confirmed `#8805` mechanism (spike `dagmar-2c68` resolved
  2026-08-05): the agent pod supplies a per-Project PAT through a headless `git credential`
  helper; the engine injects it as a session-scoped secret for that fetch and holds no standing
  credential (see §4 D10). dagmar's own private repo is the first dogfood case.
- The `ProjectManifest` grows an os-eco section (issues + expertise bindings as commands); the
  LLM tool-set becomes partially manifest-derived, contingent on the os-eco tool-wrapping spike
  (`dagmar-e8f3`) confirming data-driven tool registration in the primitive.
- One spike remains open: `dagmar-e8f3` (os-eco tool-wrapping). `dagmar-2c68` (engine-git-creds) is
  resolved — see §4 D10.

## Deferred

- **Event & trigger model** (Phase 3 autonomy, ADR-0012 §5) — a separate seed.
- **`dagmar-e8f3`** — os-eco tool-wrapping (decides primitive feasibility: data-driven tool
  registration; then the hermeticity/argv mechanics).
- **Cache-vol reclaim policy + ownership** (D4) — a later increment.
- **Object-storage backend selection + `artifactRef` schema** (D11) — when the first increment
  outgrows CR-only state.
