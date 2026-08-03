# ADR-0012: Self-bootstrap & dogfooding trajectory

Date: 2026-08-04
Seed: dagmar-e795 (part of dagmar-80dd) · Status: **PROPOSED (revised — pending re-review or acceptance)**
Reviewed: `docs/review/10-2026-08-04-c71f6cd-e795.md` (found GAP-1/GAP-2/FIX-1 + SPEC-1/2; this revision
addresses all of them — see inline "review-10" notes).

## Context

`dagmar-e795` asks: what is the minimal, hand-built core of dagmar that can then take over
developing itself, and what is the growth trajectory? This fixes the MVP shape and the order
the rest of the map (the wayfinder `dagmar-80dd` "Not yet specified" menu) resolves in. Decided
via grilling 2026-08-04; revised after dagmar-review 10.

Dogfooding is **structural, not a special case**: os-eco ports are bound per-Project with N+1
contexts (dagmar-own + each target Project — CONTEXT.md), so dagmar develops itself AS a target
Project — a configuration of the same system, not a separate path. The full execution model is
decided (ADRs 0001–0011); the cbb8 spike validated engine-nesting + `kube-pod://`-dispatch (Q3
in kind); the Go skeleton stands (ADR-0010) with `Sandbox`/`ProbeNet`/`ProbeCache` verticals.

## Decision

### 1. Substrate = local kind cluster; CRDs real from the start; trajectory kind → real → triggers → in-cluster

The bootstrap runs on a local **kind** cluster (the proven-reliable spike host; k3s-in-Dagger
was flaky — mx-76cbf6). CRDs are **real from the start**, not stubbed: registering dagmar-own
as a Project requires the Project-CRD (ADR-0002) — dogfooding must exercise the real model, not
a parallel one. Trajectory: build CRDs + controller step by step on kind → switch to a real
cluster → wire triggers → once stable, **develop further in-cluster** (deepest dogfood). The
root module (`github.com/denkhaus/dagmar`, empty per ADR-0010 §1) gets its content now: CRD
types (`api/v1alpha1/`) + controller.

### 2. Bootstrap boundary (first vertical) = the dispatch vertical, without LLM

The first end-to-end vertical proves the genuinely-new, unbuilt part: **control-plane glue.**
Project(dagmar-own) + Run-CR + a controller that reconciles a *human-created* Run → provisions
an agent pod (ServiceAccount + `pods/exec` RBAC + cache-vol + `RUNNER_HOST=kube-pod://<engine>`,
per ADR-0008) → the agent pod invokes the dagmar module (the existing `sandbox`/`probe-net`) →
the controller reports status back into the Run-CR. The LLM (Dagger primitive, proven via
greetings-api mx-e05f90) and the engine (cbb8 ✓) are NOT the new part; CRD→Controller→
`kube-pod://`-dispatch is. The coder-loop (cognition) is vertical #2, on the proven dispatch
foundation — de-risked.

**Phase-0 reads (load-bearing, IN scope — NOT deferred; review-10 GAP-1).** The dispatch
vertical's controller must provision/read two things earlier sweeps left unspecified: (1) the
**agent-pod image** — the OCI image the agent pod runs (a dagmar default, overridable per
Project); without it the pod has nothing to run; (2) the **module ref** — which dagmar module
version the agent pod invokes, and how it is pointed at the engine on kind. Both are
MVP-necessary. The ProjectManifest and the os-eco binding remain deferred (NOT needed for the
dispatch vertical).

**Reconciliation scope (Phase 0 = minimal reconcile; review-10 SPEC-1).** The controller
observes the agent pod and writes **status conditions** back into the Run-CR — the first
reconciliation semantics, and the prove-the-glue signal. Deferred to the control-plane-design
seed: requeue policy, error-backoff, finalizers, and the full reconciliation semantics. (Status
conditions ARE a reconciliation semantic; this ADR bounds which ones land in Phase 0.)

### 3. Controller fidelity = lean controller-runtime, growing

A minimal **controller-runtime** reconciler (the Kubebuilder scaffold, initially without
webhooks / leader-election / conversion). Reconciles Run → agent pod → module call → status.
CRDs are generated from Go types (K8s-standard). It grows incrementally into the full operator
(webhooks etc. later). Stays on the standard path — no throwaway hand-rolled reconciler, no
premature full-Kubebuilder heaviness. Matches the "build up step by step" trajectory ethos.

### 4. First earned capability = dagmar's own CI, as Dagger functions (always-Dagger)

After the dispatch vertical, dagmar earns its **own CI** — the deterministic safety net. The
gate-family from ADR-0009 §2 is implemented as **Dagger functions**, in the Project's Dagger-SDK
language:

- **`dagmar-bootstrap`** — the prepare step (dep/tool install).
- **`dagmar-gate`** — the verify step (build/test/lint).

These names are the **correct lifecycle stages** (retained from ADR-0009); they are now Dagger
functions, not Justfile targets.

**Always-Dagger — not flexible transport.** An earlier draft of this ADR proposed a flexible,
manifest-declared transport (a Dagger function for Go, `bun` for TypeScript, `just` for Justfile
projects). **Dropped.** Dagger SDKs cover every language that matters to dagmar (Go, TypeScript,
Python, PHP), so a conforming Project always exposes `dagmar-bootstrap` + `dagmar-gate` as
**Dagger functions** in its own SDK language — no Justfile, no `just`, no language-specific
wrapper. One mechanism, not a transport matrix.

The gate-family is **reused** both in **CI** (GitHub Actions, `dagger/dagger-for-github`,
`dagger call dagmar-gate --source=.`) and **in-loop** (coder self-verification) — CONTEXT.md's
"reused checkable," now concrete. Order: CI → cognition → housekeeping → features.

**Hermeticity carve-out (review-10 GAP-2).** ADR-0011 §2 withholds the **raw `container` tool**
from hermetic coders (N6: `container.WithExec()` always has network). `dagmar-gate` is a **named
Dagger function**, not the raw `container` tool — so it is NOT withheld. The in-loop
`dagmar-gate` runs build/test/lint via container-exec, whose network residual is **exactly the
residual ADR-0011 §3 consciously accepts** ("a raw exec path: the in-loop checkable, a build
step"). Hermetic coders therefore DO run `dagmar-gate` in-loop (self-verification preserved);
the withheld tool is the raw `container`, not the named gate function. (On acceptance, ADR-0011
§2 gains a one-line note: the checkable `dagmar-gate` is a named Dagger function, distinct from
the withheld raw `container` tool.)

> **Consequence (applies ON ACCEPTANCE): ADR-0009 §2 is REASSIGNED, not softened (review-10
> FIX-1).** §2 currently makes `dagmar-bootstrap`/`dagmar-gate` **Justfile targets** with `just`
> a mandatory conformance dependency (review 05 B2). The always-Dagger model **reassigns the
> wrapper mechanism**: Justfile targets → **Dagger functions**. The names `dagmar-bootstrap` /
> `dagmar-gate` are **retained** (correct lifecycle stages); only the mechanism changes
> (Justfile → Dagger). §2's `manifest = what` separation is preserved; only the wrapper (how)
> changes. `just` drops from "mandatory conformance dependency" to **not used**. The conformance
> requirement **strengthens**: a conforming Project must be a Dagger module exposing these two
> functions (heavier than a Justfile, but the Dagger SDKs make it feasible across Go/TS/Python/
> PHP). CONTEXT.md's Tier-A `checkable` glossary is sharpened on acceptance (the checkable IS
> `dagmar-gate`, a Dagger function; always-Dagger). These edits land at ACCEPTED, not before.

### 5. Growth trajectory = cognition before autonomy (4 phases; productionization alongside)

The "Not yet specified" menu resolves in this order, driven by "what each capability unlocks":

- **Phase 0 — Control-plane glue** (this ADR's bootstrap boundary, §2/§3): CRD types + lean
  controller + dispatch, on kind. [menu #2 partial — remainder is the control-plane-design seed]
- **Phase 1 — Own CI / gate-family** (first earned capability, §4): `dagmar-bootstrap` +
  `dagmar-gate` Dagger functions + GitHub-Actions wiring. [menu #9] — every later change is
  gated by this.
- **Phase 2 — Cognition (coder-loop vertical):** Loop-wrapping + workspace/repo model + os-eco
  adapter implementations (sd/ml/cn), bundled. The coder self-verifies via `dagmar-gate`
  (in-loop). [menu #1, #3, #4]
  - **Sub-order within Phase 2 (review-10 SPEC-2):** **Loop-wrapping + workspace/repo model
    FIRST, then os-eco adapter implementations** — the adapters are validated by the consuming
    coder-loop (a chicken-and-egg guard against building os-eco in a vacuum).
- **Phase 3 — Autonomy:** trigger model (GitHub webhooks) + merge-policy → **reactive**; then
  proactive housekeeping (cron). [menu #6, #8, #7]
- **Alongside — Productionization:** kind → real cluster (deploy/RBAC/Helm); state/persistence.
  [menu #5, #10]

**Cognition before autonomy** (Phase 2 before 3): dagmar earns the hard capability (LLM coding)
on kind — safe, human-triggered, gated by the just-earned CI — BEFORE it reacts to events. This
honors ADR-0006 (autonomy must be earned). Reactivity-before-cognition is rejected: reacting to
events dagmar cannot yet handle well is riskier than it is worth.

## Alternatives considered

- **Full Hybrid-C from day one (build the whole control plane before self-development).**
  Rejected — enormous hand-built core before the first self-Run; K8s setup blocks every step.
  Dispatch-vertical-first proves the new part incrementally.
- **Dispatch vertical WITH LLM (cognition + dispatch at once).** Rejected — two unbuilt things
  at once; harder to isolate what breaks. Dispatch-first de-risks cognition.
- **Full Kubebuilder scaffold from day one.** Rejected — too heavy for a kind bootstrap; lots of
  machinery before the first dispatch proves anything.
- **Hand-rolled reconciler (no controller-runtime).** Rejected — re-invents CRD-gen /
  status-conditions / leader-election; likely a throwaway migration to controller-runtime later.
- **CI in-loop-only (gate built inline, never standalone).** Rejected — no independently-useful
  capability; cognition before the deterministic net is independently proven.
- **Housekeeping-first.** Rejected — does not build the CI foundation that gates everything else;
  issues without a verifiable build.
- **Reactivity-before-cognition.** Rejected — reacts to events it cannot yet handle; contradicts
  ADR-0006 (autonomy = earned).
- **os-eco as its own phase between CI and cognition.** Considered — cleaner separation, but the
  adapters are hard to validate without a consuming coder-loop (chicken-and-egg); bundled into
  Phase 2 instead (with Loop+workspace first).
- **Flexible checkable transport (Dagger-fn | bun | just per language).** Rejected — Dagger SDKs
  cover Go/TS/Python/PHP, so always-Dagger is feasible and removes the transport matrix entirely.
  `dagmar-bootstrap` / `dagmar-gate` are always Dagger functions.

## Consequences

- **Root module** (`github.com/denkhaus/dagmar`) gains content: `api/v1alpha1/` (Project, Run,
  …) + `cmd/dagmar-controller/` (lean controller-runtime). No longer empty (deferred in ADR-0010
  §1; this ADR is its seed).
- **Deliberate type duplication (review-10 HOUSE-2):** root `api/v1alpha1/` CRD types and
  `.dagger/internal/domain/` domain types overlap by design — CRD types are the declarative k8s
  surface; domain types are the execution/Dagger surface; they map at the controller/module
  boundary (ADR-0010 §2). Not a DRY violation.
- **wayfinder `dagmar-80dd`** "Not yet specified" menu is now **ordered** by phase (P0–P3 +
  alongside) via §5 — no longer an unordered list. Menu #2 (control-plane design) is covered
  **partially** by Phase 0 (review-10 HOUSE-4); the remainder (concurrency, webhook admission,
  full reconciliation) is the control-plane-design seed, filed out of Phase 0.
- **In-cluster self-development recursion (review-10 HOUSE-3):** the trajectory's "develop
  in-cluster" end-state is recursive (dagmar develops itself inside its own production cluster).
  Bounded by the CI gate-family + ADR-0006 (autonomy earned); noted as a conscious residual, not
  addressed further here.
- **ADR-0009 §2 reassigned on acceptance** (see §4 consequence): Justfile targets → Dagger
  functions; `just` not used.
- **ADR-0011 §2** gains a one-line carve-out note on acceptance (the checkable `dagmar-gate` ≠
  the withheld raw `container` tool).
- **CONTEXT.md** checkable glossary sharpened on acceptance (checkable = `dagmar-gate`, a Dagger
  function; always-Dagger); ADR list gains 0012.
- **Dogfooding trajectory fixed:** Phase 0 → 1 → 2 → 3, productionization alongside. dagmar
  develops itself as dagmar-own (a registered Project) once Phase 2 lands.
- **ProbeNet / ProbeCache** (existing spikes) feed Phase 0 (engine + cache-isolation evidence)
  and Phase 1 (the gate's build/test/lint run via container exec).

## Deferred to the control-plane-design seed (filed out of Phase 0)

Dispatch concurrency, Workspace-lineage sequencing, webhook admission, the controller's full
reconciliation semantics (requeue/error-backoff/finalizers), and the agent-pod-image /
module-ref provisioning details beyond the Phase-0 MVP reads. This ADR fixes the *trajectory and
fidelity*, not those internals.
