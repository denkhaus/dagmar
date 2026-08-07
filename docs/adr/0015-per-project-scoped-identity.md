# ADR-0015: Per-Project-scoped agent identity & D4 finalizer unblock

Date: 2026-08-07
Seed: dagmar-54c9 (part of dagmar-80dd) · Status: **ACCEPTED**
Decided via grilling 2026-08-07; revised after dagmar-review 20
(`docs/review/20-2026-08-07-1bb4cad-54c9.md`, NEEDS-WORK-light → revised: GAP-1 RBAC ledger,
SPEC-1 third-mechanism honesty, SPEC-2 lifecycle-vs-credential disclaimer, HOUSE-1/2/3/4; then
accepted). Resolves the identity-refactor seed: makes agent identity safely per-Project-deletable
so ADR-0013 §2 **D4** (a Project finalizer that tears down provisioned identity) is unblocked, and
closes two live hazards — the SA-owner-ref GC sibling-break (review-19 GAP-1) and the
engine-namespace Role/RoleBinding leak (carried from dagmar-67bc).

## Context

`dagmar-54c9` asks: how does agent identity become safely per-Project-deletable, so a finalizer
can tear it down without breaking sibling Runs? ADR-0013 §2 D4 (a Project-level identity
finalizer) is **deferred** — its "Implementation reality (2026-08-06)" note records that identity
is provisioned per-Run but **shared-named** within a namespace, so a finalizer deleting it would
break siblings. The note also records a hazard that is **live today**, not just future-finalizer:
the shared-named `dagmar-agent` SA is controller-owner-ref'd to its *creating* Run
(`RunReconciler.ensureAgentIdentity`, `ctrl.SetControllerReference`), so deleting that Run
**already cascades via k8s GC** — the SA vanishes and every sibling Run's pods (which run as that
SA) lose their identity (admission failures, broken exec-into-engine). Separately, the engine-ns
`dagmar-agent-exec` Role + RoleBinding are cross-namespace (cannot be owner-ref'd to a Run) and
**leak** on Run deletion (dagmar-67bc interim TODO).

**Already decided (do not re-litigate):**

- CRD set + Run/Project model (ADR-0002); the RunReconciler + `ensureAgentIdentity` shape
  (ADR-0012 §2/§3, Phase 0).
- Reconciliation model: owner-ref pod watch (D3, done), `metav1.Condition` pinned set (D5, done),
  Project-level identity finalizer (D4, **deferred → this ADR unblocks it**).
- The singleton Dagger engine runs in namespace `dagmar`; agent SAs need `pods/exec` + `pods:get`
  on it, so that RBAC MUST live in the engine namespace (Role/RoleBinding are namespaced) — every
  identity option carries a cross-ns engine-ns RoleBinding the finalizer must clean.
- ADR-0008 §4 mandates per-Project namespaces as the hard credential trust boundary.

**Out of scope (separate seeds):** per-Project **namespace provisioning** (ADR-0008 §4) — this ADR
deliberately leaves that divergence open as an interim (Decision §5); the cache-vol reclaim policy
(ADR-0013 D4's other deferred half, review-15 HOUSE-4).

## Decision

### 1. Identity granularity — per-Project, single fixed namespace (Q1 = C)

"Agent identity" here means the pod's **exec-into-engine identity**: the ServiceAccount the agent
pod runs as + the engine-namespace Role/RoleBinding granting it `pods/exec`+`pods:get` on the
singleton engine — *distinct from* ADR-0007's per-Run projected credentials (the SA carries no
`secrets` verbs; it is not a credential holder). This ADR concerns the identity's **lifecycle
granularity**, not credential isolation (§5).

Agent identity becomes **per-Project-scoped**: a ServiceAccount `dagmar-agent-<project>` in the
Project's namespace (today `default`), shared by all Runs of that Project. The engine-namespace
Role + RoleBinding become `dagmar-agent-exec-<project>` (uniquely named per Project). Granularity
matches the trust boundary (identity per-Project, like the eventual namespace per-Project) and
matches ADR-0013 D4's **Project-level** finalizer as written — no D4 revision needed.

All Runs of a Project share the one per-Project SA. Workspaces stay per-Run-isolated (CONTEXT.md);
the SA is the Project's identity, not a per-Run one. This is the intended isolation boundary until
per-Project namespaces land (§5), at which point the SA simply moves into the Project's own
namespace — same name, same RBAC *shape* (the RoleBinding `subject.Namespace` and the SA's
namespace do change — minor string rework, no identity-model rework).

### 2. Ownership — a new ProjectReconciler owns the whole identity lifecycle (Q2 = C1-full)

A new **`ProjectReconciler`** (does not exist today) owns identity creation AND deletion
end-to-end. `RunReconciler.ensureAgentIdentity` is **removed**; the RunReconciler becomes a
consumer only.

- **On Project create/observe:** the ProjectReconciler sets a finalizer
  `dagmar.denkhaus.io/identity-protection`, then creates the SA `dagmar-agent-<project>`
  (controller-owner-ref'd to the **Project**, not a Run), and the engine-ns
  `dagmar-agent-exec-<project>` Role + RoleBinding (cross-namespace, so NOT owner-ref'd).
- **On Project delete** (`DeletionTimestamp` set): the finalizer deletes the engine-ns Role +
  RoleBinding (retry-with-backoff on transient error — standard controller pattern; the Project
  stays `Terminating` until cleanup succeeds). The SA is GC'd automatically via its owner-ref to
  the Project. The finalizer is then removed.
- **RunReconciler:** `ensureAgentIdentity` is deleted; a `Get` of `dagmar-agent-<project>` replaces
  it. `NotFound` → requeue (the ProjectReconciler has not run yet; a sub-second race window,
  caught by one requeue). No identity creation in the Run path.

**SoC:** one controller owns Project-scoped identity end-to-end; the RunReconciler owns only the
Run lifecycle + its pod. This is the cleaner separation over (C2-split), where creation stayed
lazy in the RunReconciler and only the finalizer lived on the Project — split ownership of one
per-Project resource.

**Project status (ADR-0013 D5):** the ProjectReconciler writes D5's pinned Project conditions —
`Provisioned=True` once the SA + engine-ns Role/RoleBinding are reconciled, `Ready=True` once the
binding is in place — so `projects/status` is **required** (not optional). (Today `ProjectStatus`
exists but is written by nothing; this is its first writer.)

### 3. SA owner-ref → Project (not Run) — closes review-19 GAP-1

The SA's controller owner-reference points at the **Project**, not at any Run. This is the core
fix for the live hazard (review-19 GAP-1): deleting a Run can no longer GC the SA (a Run does not
own it); only deleting the Project cascades, which is exactly the intended lifecycle. The
per-Project naming makes this safe — the SA's lifecycle is the Project's lifecycle.

### 4. Finalizer cleans the cross-ns engine Role/RoleBinding — closes the dagmar-67bc leak

The engine-ns `dagmar-agent-exec-<project>` Role + RoleBinding are cross-namespace, so they
cannot be owner-ref'd to the Project (k8s forbids cross-ns owner refs). They are therefore
**finalizer-cleaned** (§2, on Project delete). Because they are now per-Project-named (not the
shared `dagmar-agent-exec`), deleting one Project's binding cannot affect another Project — the
finalizer is safe to run. This closes the carried-over dagmar-67bc leak and removes its interim
TODO in `ensureAgentIdentity` (which is deleted outright under §2).

### 5. ADR-0008 §4 divergence — deliberate interim (Q3 = I)

This ADR scopes **identity only**. Per-Project **namespace provisioning** (ADR-0008 §4, mandatory)
is NOT done here: Runs continue to run in the Project's namespace (today `default`), and the
per-Project SA is the trust-boundary proxy meanwhile. The divergence from ADR-0008 §4 is recorded
as a **deliberate interim** (mirroring the ADR-0014 GAP-1 / ADR-0013 D4 reality-note honesty
pattern) and is lifted by a separate follow-on namespace-provisioning seed. Forward-compatible:
when per-Project namespaces land, the SA moves into the Project's own namespace — same name, same
RBAC shape, no identity-model rework.

This keeps the increment right-sized: the two acute hazards (review-19 GAP-1; dagmar-67bc leak)
are **identity** problems, not namespace problems — fixing them needs no namespace provisioning.
Bundling namespace provisioning (option B) would couple a safety fix to a larger migration (Run
placement changes, controller namespace assumptions, samples/RBAC rewrite, the 63-char
namespace-name limit + slug strategy) without unblocking D4 any further.

**Scope disclaimer (lifecycle-only, not credential isolation):** per-Project *naming* improves
cross-Project identity separation within the shared namespace (Project A's pods cannot bind
Project B's exec RoleBinding), but it delivers **no** cross-Project *credential* isolation —
every Project still runs in `default`, so the namespace-scoped defense-in-depth layer
(ADR-0007 §1, ADR-0008 §4) remains entirely absent, exactly as today. This ADR provides safe
identity **lifecycle**; the credential boundary stays wholly on the deferred namespace layer.
(This realizes one slice of ADR-0007's deferred "exact RBAC RoleBindings /
service-account-per-Project model" — orthogonal to ADR-0007 §5's per-Run projected-secret
isolation, which is untouched.)

## Alternatives considered

*Granularity (Q1):*

- **(A) Per-Run-named identity** (`dagmar-agent-<run>`): rejected — identity finer than the
  Project trust boundary (ADR-0008 §4), and it contradicts ADR-0013 D4's **Project-level**
  finalizer (would require revising D4 to Run-level, or an awkward Project-finalizer-iterating-Runs
  shape). Smallest diff, but fights both the trust boundary and the accepted D4.
- **(B) Full per-Project namespace provisioning now:** rejected — bundles a safety fix with a
  larger architecture migration; closes the ADR-0008 §4 divergence in one shot but at the cost of
  Run-placement changes, namespace-assumption rework, samples/RBAC rewrite, and the 63-char
  namespace-name limit (Project-name → namespace-name needs a slug/truncation strategy). The
  hazards this seed must fix are identity-scoped, not namespace-scoped.

*Ownership (Q2):*

- **(C2-split) RunReconciler stays lazy, ProjectReconciler finalizer-only:** rejected — split
  ownership of one per-Project resource (creation in one controller, deletion in another);
  less-coherent SoC than (C1-full) for ~equal code.

*Baseline:*

- **Status quo (shared-named, no finalizer):** rejected — leaves both live hazards open
  (review-19 GAP-1 sibling-break; dagmar-67bc leak) and leaves D4 blocked indefinitely.

## Consequences

- A new `ProjectReconciler` is added (`For(&Project{})`); `RunReconciler.ensureAgentIdentity` is
  deleted (~40 lines), replaced by a `Get` of `dagmar-agent-<project>` + requeue-on-NotFound.
- **RBAC ledger (the ProjectReconciler gains what the RunReconciler relinquishes):** the
  ProjectReconciler carries `projects/finalizers:update` (the finalizer), `projects/status`
  (required — it writes the D5 Project conditions, §2), and the **migrated**
  `serviceaccounts`/`roles`/`rolebindings` `get;list;watch;create;update;patch` verbs the
  RunReconciler drops. The existing `projects:get;list;watch` stays. Note the Role/RoleBinding are
  created in the **engine** namespace — dagmar's first cross-namespace create by a reconciler
  (the Project's namespace is `default`, the engine's is `dagmar`).
- **D4 (ADR-0013 §2) unblocked — by a third mechanism:** the Project-level finalizer now safely
  tears down identity (per-Project-named → no sibling break). The realized branch is neither of
  the D4 deferral note's binary (per-Run-named OR per-Project-namespace) — it is per-Project-named
  identity in a shared namespace + a ProjectReconciler, a hybrid that satisfies the note's
  substantive requirement (drop/re-scope the SA owner-ref). Once implemented, ADR-0013 §2's
  "Implementation reality — D4 deferred" note is lifted by recording this actual mechanism (not
  "the binary was resolved").
- **Both live hazards closed:** the SA-owner-ref GC sibling-break (review-19 GAP-1) and the
  engine-ns Role/RoleBinding leak (dagmar-67bc, TODO removed).
- **`SetupWithManager`:** the Run reconciler does NOT add `Owns(&ServiceAccount{})` (the SA is
  Project-owned now — a Run owner-ref watch is semantically wrong; the D5 implementation note
  already called this out, and its stated condition should be updated from "until per-Run-named"
  to the per-Project reason). The **Project** reconciler registers `For(&Project{}) +
  Owns(&ServiceAccount{})` — the SA is Project-owned, so SA changes requeue the Project.
- **Migration (two distinct cases):** the dogfood cluster carries stale shared-named identity.
  - The old `dagmar-agent` SA is controller-owner-ref'd to its *creating* Run, so it **self-cleans**
    when that Run deletes — not an orphan, no manual sweep needed.
  - The old engine-ns `dagmar-agent-exec` Role + RoleBinding are cross-namespace, not owner-ref'd,
    and carry no finalizer → they **leak indefinitely** (literally the dagmar-67bc bug, in the old
    name) until a manual `kubectl delete -n dagmar role,rolebinding dagmar-agent-exec`. Call this
    out, do not euphemize it as "harmless duplicate."
  Documented as a migration TODO for any cluster that ran the pre-0015 controller.

## Deferred

- **Per-Project namespace provisioning** (ADR-0008 §4) — a separate follow-on seed; lifts the §5
  interim. The `ProjectReconciler` this ADR adds is the natural home for it (namespace create +
  the SA inside it).
- **Cache-vol reclaim policy + ownership** (ADR-0013 D4's other deferred half, review-15
  HOUSE-4) — separate.
- **The implementation itself** — the `ProjectReconciler`, the RBAC markers, the
  `ensureAgentIdentity` removal, the migration sweep. This ADR fixes the decision; a follow-on
  implementation increment (with its own review) realizes it.
