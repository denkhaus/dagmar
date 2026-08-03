# ADR-0012: Self-bootstrap & dogfooding trajectory

Date: 2026-08-04
Seed: dagmar-e795 (part of dagmar-80dd) · Status: **PROPOSED (draft — pending review)**

## Context

`dagmar-e795` asks: what is the minimal, hand-built core of dagmar that can then take over
developing itself, and what is the growth trajectory? This fixes the MVP shape and the order
the rest of the map (the wayfinder `dagmar-80dd` "Not yet specified" menu) resolves in. Decided
via grilling 2026-08-04.

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

### 3. Controller fidelity = lean controller-runtime, growing

A minimal **controller-runtime** reconciler (the Kubebuilder scaffold, initially without
webhooks / leader-election / conversion). Reconciles Run → agent pod → module call → status.
CRDs are generated from Go types (K8s-standard). It grows incrementally into the full operator
(webhooks etc. later). Stays on the standard path — no throwaway hand-rolled reconciler, no
premature full-Kubebuilder heaviness. Matches the "build up step by step" trajectory ethos.

### 4. First earned capability = dagmar's own CI, as a checkable with flexible transport

After the dispatch vertical, dagmar earns its **own CI** — the deterministic safety net. The
checkable is **`quality_gate`**, with a **flexible, manifest-declared transport**: Dagger is
always the *harness* (runs the checkable, in-loop + CI, captures exit code); the checkable
*body* is language-idiomatic:

- **dagmar-own (Go):** `quality_gate` as a Dagger function (the idiomatic ideal — "child's play").
- **TypeScript project:** `bun quality_gate` (a Dagger function would feel imposed).
- **Justfile project:** `just quality_gate`.

The single checkable is **reused** both in **CI** (GitHub Actions, `dagger/dagger-for-github`,
`dagger call quality_gate --source=.`) and **in-loop** (coder self-verification) — CONTEXT.md's
"reused checkable," now concrete. Order: CI → cognition → housekeeping → features.

> **Consequence (applies ON ACCEPTANCE): ADR-0009 §2 softens.** §2 currently makes `just
> dagmar-gate` (Justfile) the mandatory gate-conformance dependency (review 05 B2). The
> flexible-transport model revises this: conformance = "expose a checkable in the manifest,"
> not "expose a Justfile target." `just` drops from "mandatory conformance dependency" to "one
> option." CONTEXT.md's Tier-A `checkable` glossary gains a one-line sharpening (Dagger = harness;
> body transport flexible). These edits land when this ADR is ACCEPTED, not before.

### 5. Growth trajectory = cognition before autonomy (4 phases; productionization alongside)

The "Not yet specified" menu resolves in this order, driven by "what each capability unlocks":

- **Phase 0 — Control-plane glue** (this ADR's bootstrap boundary, §2/§3): CRD types + lean
  controller + dispatch, on kind. [menu #2 partial]
- **Phase 1 — Own CI / `quality_gate`** (first earned capability, §4): Dagger function +
  GitHub-Actions wiring. [menu #9] — every later change is gated by this.
- **Phase 2 — Cognition (coder-loop vertical):** Loop-wrapping + workspace/repo model + os-eco
  adapter implementations (sd/ml/cn), bundled. The coder self-verifies via `quality_gate`
  (in-loop). [menu #1, #3, #4]
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
- **CI in-loop-only (checkable built inline, never standalone).** Rejected — no
  independently-useful capability; cognition before the deterministic net is independently proven.
- **Housekeeping-first.** Rejected — does not build the CI foundation that gates everything else;
  issues without a verifiable build.
- **Reactivity-before-cognition.** Rejected — reacts to events it cannot yet handle; contradicts
  ADR-0006 (autonomy = earned).
- **os-eco as its own phase between CI and cognition.** Considered — cleaner separation, but the
  adapters are hard to validate without a consuming coder-loop (chicken-and-egg); bundled into
  Phase 2 instead.
- **Rigid checkable transport (`just` mandatory, or Dagger-fn mandatory).** Rejected —
  multi-language projects need a flexible, manifest-declared transport; a Dagger function is the
  idiomatic ideal only where it fits (Go).

## Consequences

- **Root module** (`github.com/denkhaus/dagmar`) gains content: `api/v1alpha1/` (Project, Run,
  …) + `cmd/dagmar-controller/` (lean controller-runtime). No longer empty (deferred in ADR-0010
  §1; this ADR is its seed).
- **wayfinder `dagmar-80dd`** "Not yet specified" menu is now **ordered** by phase (P0–P3 +
  alongside) via §5 — no longer an unordered list.
- **ADR-0009 §2 softens on acceptance** (see §4 consequence): `just` mandatory → flexible
  transport.
- **CONTEXT.md** checkable glossary sharpened on acceptance (Dagger = harness; body flexible);
  ADR list gains 0012.
- **Dogfooding trajectory fixed:** Phase 0 → 1 → 2 → 3, productionization alongside. dagmar
  develops itself as dagmar-own (a registered Project) once Phase 2 lands.
- **Deferred to the control-plane-design seed (filed out of Phase 0):** dispatch concurrency,
  Workspace-lineage sequencing, webhook admission, the controller's full reconciliation
  semantics. This ADR fixes the *trajectory and fidelity*, not those internals.
- **ProbeNet / ProbeCache** (existing spikes) feed Phase 0 (engine + cache-isolation evidence)
  and Phase 1 (the checkable's build/test/lint run via container exec).

## Open during review (this draft's derivations — please confirm or correct)

- **§4 ADR-0009 §2 softening:** confirm `just` drops from "mandatory conformance dependency" to
  "one option" (flexible transport), vs. keeping `just` mandatory and layering `quality_gate`
  over it.
- **§5 Phase 2 bundling** (#1 Loop-wrapping + #3 workspace/repo + #4 os-eco adapters together):
  confirm the bundle vs. sequencing them.
- **§2 scope:** confirm the dispatch-vertical reports status back into the Run-CR (status
  conditions) as the first controller semantics, not just pod creation.
