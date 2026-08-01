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
- The project exposes standardized entry points — concretely **Justfile targets**
  `dagmar-bootstrap` (prepare the workspace; install tools/deps) and `dagmar-gate` (run the
  checkables: build/test/lint + rules/thresholds + static analysis). These are the project's
  conformance contract for deterministic execution (analogous to the ProjectManifest,
  ADR-0003).
- **Relationship to the ProjectManifest (ADR-0003):** the manifest declares **what** the
  checkables are (its `checkables:` section); `dagmar-gate` is the **execution wrapper**
  that runs them — manifest = *what*, Justfile target = *how to invoke*. `dagmar-gate` does
  **not** re-declare checkables; it is the single deterministic entry point that consumes
  the manifest-declared checkables, so the manifest stays authoritative for "the
  checkables" (no projection ambiguity). This makes **`just` (Justfile) a mandatory runtime
  tool for every conforming Project** — a conformance dependency introduced here, not in
  ADR-0003.

### 3. Hermetic LLM via the tool-set (trust zone)

- The LLM agent loop runs **hermetically — no network**. This is enforced through the
  **tool-set** (the Agent CRD `tool-set` field, which dagmar chooses freely per role): a
  hermetic agent's tool-set simply excludes any network-capable tool.
- `dagmar-bootstrap` and `dagmar-gate` (deterministic) **have network** (to install
  deps/tools/services).
- Rationale: the LLM processes untrusted content (repo/issues/PRs → prompt-injection risk);
  hermetic prevents exfiltration and tool-install. The full Sandbox trust-zone model is a
  separate ADR (it sharpens ADR-0007's encapsulation boundary).
- **Capability-boundary dependency (GAP):** "excludes any network-capable tool" is the
  *principle*, not a pinned surface — the Tool glossary lists `container` and `git` as
  network-capable, and whether those (or only `http`) are withheld for hermetic agents — and
  whether hermeticity is additionally enforced by a Sandbox-pod NetworkPolicy — is defined by
  the **forthcoming Sandbox trust-zone ADR** (→ seed `dagmar-911b`; the most consequential
  missing ADR — review 2026-08-01-mache §C1). ADR-0009 asserts the enforcement *mechanism
  class* (tool-set exclusion), not the resolved per-tool boundary.

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

- **Glossary:** `dagmar-bootstrap` / `dagmar-gate` Justfile targets = the project's
  deterministic gate conformance contract; ReviewAgent = one cognitive role (specialists
  later); Calibration loop = disagreement → `review-calibration` mixin → co-evolution;
  post-merge watchdog = filtered safety-net workflow.
- **Project contract:** a Project must expose `dagmar-bootstrap` and `dagmar-gate` (Justfile
  targets) as its gate conformance. These **wrap** the manifest-declared checkables
  (ADR-0003) — manifest = *what*, `dagmar-gate` = *how to run* — so `just` (Justfile) is a
  mandatory runtime tool for every conforming Project (conformance dependency introduced
  here, not in ADR-0003).
- **ADR-0006:** used/confirmed (two-green, veto, Calibration Agent workflow). **ADR-0005:**
  `review-calibration` mixin composition. **ADR-0003:** Justfile-target gate conformance.
- **Spun out (separate seeds/ADRs):** general Workflow-CRD framework; Sandbox trust-zones /
  hermetic-LLM network segmentation (sharpens ADR-0007); Wayfinder Destination update toward
  "Dagger-based software factory".
- **Deferred:** Calibration auto-apply (operator-approved initially); specialist ReviewAgents;
  concurrency / Workspace-lineage sequencing (control-plane).
