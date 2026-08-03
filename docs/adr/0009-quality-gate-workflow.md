# ADR-0009: Quality-gate workflow family

- **Status:** decided
- **Date:** 2026-08-01
- **Resolved in:** seeds dagmar-e95b
- **Evidence:** grilling session 2026-08-01 (seed `dagmar-e95b`); builds on ADR-0006 (autonomy / two-green / Calibration), ADR-0005 (prompt composition), ADR-0003 (ProjectManifest conformance).

## Context

`dagmar-e95b` owns the **workflow** layer of autonomy safety: the coder→review→gate→delivery
flow, what "delivered" means, the gate-evolution mechanism, the Calibration-Agent workflow,
and the post-merge watchdog. The **policy** layer was decided in ADR-0006 (merge is a
deterministic controller Dagger function; two green lights — `QualityGate.green ∧
ReviewAgent.approve`; ReviewAgent holds a hard veto; gate is invariant; Calibration Agent
deferred). This ADR does not re-litigate policy; it specifies how the workflow runs.

A grilling pass reframed dagmar as a **Dagger-based software factory**: the Workflow concept
is a thin CRD that references a Dagger Go function (general framework tracked separately),
and the quality-gate flow is **one workflow family** within it.

## Decision

### 1. Scope = the quality-gate family

This ADR covers the quality-gate **building blocks** (reused from ADR-0006: QualityGate,
ReviewAgent, two-green, Calibration Agent) and **two concrete workflows**: the review/merge
pipeline and the post-merge watchdog. The **general Workflow-CRD framework** and the
**Sandbox trust-zone / hermetic-LLM network model** are broader and tracked as separate
seeds/ADRs (the latter sharpens ADR-0007).

### 2. Gate composition = deterministic, via project-conformance entry points

- The QualityGate is **purely deterministic**. No AI-review inside the gate — that is the
  ReviewAgent's job. "Gate green" = all checkables + rules pass deterministically.
- The project exposes standardized entry points — concretely **Dagger functions**
  `dagmar-bootstrap` (prepare the workspace; install tools/deps) and `dagmar-gate` (run the
  checkables: build/test/lint + rules/thresholds + static analysis), written in the project's
  Dagger-SDK language. These are the project's conformance contract for deterministic execution
  (analogous to the ProjectManifest, ADR-0003).
- **Relationship to the ProjectManifest (ADR-0003):** the manifest declares **what** the
  checkables are (its `checkables:` section); `dagmar-gate` is the **execution wrapper** that
  runs them — manifest = *what*, Dagger function = *how to invoke*. `dagmar-gate` does **not**
  re-declare checkables; it is the single deterministic entry point that consumes the
  manifest-declared checkables, so the manifest stays authoritative for "the checkables" (no
  projection ambiguity).
- **Wrapper mechanism reassigned by ADR-0012 §4:** this section originally specified **Justfile
  targets** and made `just` a mandatory runtime tool. ADR-0012 reassigns the wrapper to
  **always-Dagger functions** (Dagger SDKs cover Go/TS/Python/PHP, so no Justfile/`just`/`bun`
  is needed); `just` drops from "mandatory conformance dependency" to **not used**, and
  conformance strengthens to "the Project is a Dagger module exposing `dagmar-bootstrap` +
  `dagmar-gate`."

### 3. Hermetic LLM via the tool-set (trust zone)

- The LLM agent loop runs **hermetically — no network**. This is enforced through the
  **tool-set** (the Agent CRD `tool-set` field, which dagmar chooses freely per role): a
  hermetic agent's tool-set simply excludes any network-capable tool.
- `dagmar-bootstrap` and `dagmar-gate` (deterministic) **have network** (to install
  deps/tools/services).
- Rationale: the LLM processes untrusted content (repo/issues/PRs → prompt-injection risk);
  hermetic prevents exfiltration and tool-install. The full Sandbox trust-zone model is a
  separate ADR (it sharpens ADR-0007's encapsulation boundary).
- **Capability-boundary dependency (RESOLVED by ADR-0011):** the per-tool boundary is now
  pinned — hermetic agents withhold the network-capable tools wholesale (`http`, `git` remote
  ops, the **entire** `container` tool, since `container.WithExec()` always has network). The
  enforcement mechanism is the tool-set (as asserted here); no Sandbox-pod NetworkPolicy is
  added (a "two-engines bell" was considered in ADR-0011 and rejected). Note: "no network" here
  is a *tool-surface* statement (no network-capable tool on the `Env`), not a literal air-gap —
  see ADR-0011 §2/§3 for the precise term and the accepted residual.

### 4. Decision flow — gate-before-review, revise loop, max-N termination

```
candidate (PR)
 → Gate (deterministic; dagmar-gate; network)
    ├─ RED   → revise (Gate feedback)
    └─ GREEN → ReviewAgent (cognitive; hermetic; review-calibration mixin)
                 ├─ APPROVE → Two-Green → Delivery
                 └─ VETO    → [disagreement: Gate-green ∧ Veto] → §5
 (each revise = a new coder Run on the same Task/PR; Gate-Red and Veto are both feedback,
  deterministic errors + cognitive critique)
 max N rounds (default 3, per-Workflow) → escalate to a human.
```

**Gate before Review** for efficiency: the deterministic gate filters cheaply, so the
expensive LLM review only runs on gate-green code. Two-green still requires **both**
(ADR-0006); the ordering is an efficiency choice.

### 5. Calibration loop on disagreement (ADR-0006; workflow owned here)

On **disagreement** (`Gate-green ∧ ReviewAgent-Veto` — the only reachable disagreement under
gate-before-review), the **Calibration Agent** (a non-gating LLM step) diagnoses the cause
(gate-gap vs reviewer-drift) and emits a `review-calibration` mixin, persisted in the
**project's canopy** via the Prompts port (ADR-0005). The ReviewAgent prompt is then composed
as `dagmar review-agent ⊕ project review-calibration ⊕ project content` (ADR-0006). Effect:
ReviewAgent and Gate **co-evolve incrementally**. Mixins are **operator-approved initially**
(auto-apply deferred, ADR-0006). It is a non-gating analyzer — **no gate-of-gates**, no
infinite regress.

### 6. ReviewAgent = one (start)

One generalist ReviewAgent role (per ADR-0006), whose focus sharpens over time via the
`review-calibration` mixin. **Specialist reviewers** (security/perf/correctness) are a later
evolution (a workflow with multiple review Runs), not the first cut — they multiply veto
conflicts and Calibration complexity.

### 7. Delivery

- `authority=auto` → the controller merges the PR on Two-Green. **Delivered = merged.**
- `authority=human` → dagmar opens the PR; Gate + Review run pre-merge as **advisory**; the
  human merges. **Delivered (dagmar's view) = PR handoff.**
- The gate is **invariant** — it always runs on the candidate, **pre-merge**. (The gate
  being non-negotiable is from ADR-0006; the **pre-merge** timing and the separate
  post-merge watchdog are specified here in §7/§8 — ADR-0006's reactive tier does *not*
  run the gate post-merge.)

### 8. Post-merge watchdog = separate workflow, filtered trigger

A **separate Workflow**, triggered by merge events but **filtered to fire only on
human/spontaneous merges** — excluding dagmar's own auto-merges and actively-triggered merges
(to avoid an infinite loop). Mechanism: dagmar marks its own merges (commit/PR metadata); the
watchdog trigger skips them. Action: run Gate + ReviewAgent on the merged HEAD (reuses the
same building blocks). Two-Green → no-op; Red/Veto → open a **fix-PR** (coder fixes the
regression → review → merge). Purpose: a **safety-net add-on** for human autonomy (take over
unreviewed merges), not a component of every workflow.

## Alternatives considered

- **AI-review thresholds inside the gate.** Rejected — the gate stays deterministic
  (reproducible; no LLM flakiness in the gate); AI-review belongs to the ReviewAgent.
- **Review-before-gate (or parallel).** Rejected — gate-first filters cheaply before the
  expensive LLM review.
- **Multiple specialist ReviewAgents from the start.** Rejected for the first cut — slim; the
  Calibration mixin sharpens one reviewer; specialists are a later evolution.
- **Watchdog as a mode of the coder workflow (not a separate workflow).** Rejected — the
  coder workflow is for dagmar-produced PRs; the watchdog is for external/human merges and
  needs a filtered trigger, so a separate workflow is cleaner.

## Consequences

- **Glossary:** `dagmar-bootstrap` / `dagmar-gate` **Dagger functions** = the project's
  deterministic gate conformance contract (reassigned from Justfile targets by ADR-0012 §4);
  ReviewAgent = one cognitive role (specialists later); Calibration loop = disagreement →
  `review-calibration` mixin → co-evolution; post-merge watchdog = filtered safety-net workflow.
- **Project contract:** a Project must expose `dagmar-bootstrap` and `dagmar-gate` as **Dagger
  functions** (i.e. the Project is a Dagger module). These **wrap** the manifest-declared
  checkables (ADR-0003) — manifest = *what*, `dagmar-gate` = *how to run*. (Originally Justfile
  targets with `just` mandatory; reassigned to always-Dagger functions by ADR-0012 §4.)
- **ADR-0006:** used/confirmed (two-green, veto, Calibration Agent workflow). **ADR-0005:**
  `review-calibration` mixin composition. **ADR-0003:** manifest-declared checkables (the
  *what*); the gate-wrapper mechanism is decided in ADR-0009 §2 / reassigned by ADR-0012 §4.
- **Spun out (separate seeds/ADRs):** general Workflow-CRD framework; Sandbox trust-zones /
  hermetic-LLM network segmentation (sharpens ADR-0007); Wayfinder Destination update toward
  "Dagger-based software factory".
- **Deferred:** Calibration auto-apply (operator-approved initially); specialist ReviewAgents;
  concurrency / Workspace-lineage sequencing (control-plane).
