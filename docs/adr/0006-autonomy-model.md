# ADR-0006: Autonomy model

- **Status:** decided (Calibration Agent design captured, implementation deferred)
- **Date:** 2026-07-31
- **Resolved in:** seeds `dagmar-fa45`

## Context

dagmar's target is full autonomy (incl. merge), bounded by an evolving quality gate and
review. Three entities seemed to carry "autonomy" (Project autonomy-level, Agent
autonomy-scope, QualityGate merge-rules) with unclear precedence. The model must be
**safe** (no premature merge), **deterministic** where it matters, **slim** (grow on
demand), and **self-improving**.

## Decision

### Gate is invariant
The QualityGate always secures quality, at every level — non-negotiable. Autonomy
governs only what dagmar does **around** a gate result (merge authority) and which
triggers it acts on.

### Two slim axes (not a fixed ladder)
1. **Merge authority** — `human` (default; dagmar opens PRs, human merges) | `auto`
   (dagmar merges on gate-pass).
2. **Trigger tier** — `on-demand` (human starts a Run) | `reactive` (events:
   issue/webhook → work) | `proactive` (cron housekeeping). The post-merge **watchdog**
   — Gate + Review run on a merged HEAD after a human/spontaneous merge — is **not** a
   reactive-tier event; it is a **separate workflow with a filtered trigger** (ADR-0009
   §8). The gate itself is **invariant and always runs pre-merge** (ADR-0009 §7).

"Autonomy level" = `{merge-authority} × {trigger-tier}`. Start minimal: `{human,
on-demand}`; grow by enabling reactive → auto-merge → proactive.

### Merge is deterministic and system-level
A **Dagger function on the controller** fires iff
`QualityGate.green ∧ ReviewAgent.approve ∧ Project.mergeAuthority == auto`. Merge is in
**no** Agent's tool-set; no LLM decides to merge. Consequently the Agent has no merge
authority — the autonomy axes are Project (system) settings, not Agent properties.

### Two green lights + veto
Merge requires BOTH:
- **QualityGate** (deterministic: checkables + rules) → green/red.
- **ReviewAgent** (cognitive, LLM) → approve / **VETO**.

The ReviewAgent holds a hard veto even when the deterministic gate is green. Defense in
depth: both must err for a bad merge; a false veto only blocks (safe failure direction).

### Growth — two independent levers
- **Gate accuracy** grows via the Calibration Agent (below).
- **Authority promotion** (human→auto, tier enablement) is **operator-driven**: a human
  flips the Project's settings when confident, informed by dashboards (veto rate, run
  history). The system never self-promotes. Auto-promotion deferred.

## Calibration Agent (deferred implementation — design captured)

A third LLM-driven step, **not a gate**: on disagreement between QualityGate and
ReviewAgent, it diagnoses the root cause (gate gap vs reviewer drift) and emits
improvement proposals. Per ADR-0005 (Variant A), calibration output is **project-specific**
→ persisted as a `review-calibration` mixin in the **project's canopy** (written via the
Prompts port); dagmar's generic `review-agent` mixin stays project-agnostic. Composed
ReviewAgent prompt = dagmar `review-agent` ⊕ project `review-calibration` ⊕ project
content. Proposals are operator-approved initially (auto-apply deferred). No infinite
regress — it is a non-gating analyzer, not a gate-of-gates. Workflow belongs to seeds
`dagmar-e95b`.

## Alternatives considered

- **Fixed 5-level autonomy ladder (L0–L4).** Rejected — too rigid/complex for the start;
  the two-axis model is slimmer and grows on demand.
- **Agent-governed merge authority ("autonomy scope").** Rejected — merge must be
  deterministic and system-level; an LLM must never decide to merge.
- **Purely deterministic gate (no ReviewAgent).** Rejected — loses cognitive review; the
  two-green model keeps the merge deterministic while using the ReviewAgent verdict as a
  co-equal gate.
- **Metric-driven auto-promotion of authority.** Deferred — operator-driven first
  (slim, safe); revisit when confidence/data exist.

## Consequences

- Glossary: Project "autonomy setting" = `{merge-authority, trigger-tier}`; QualityGate =
  deterministic layer; ReviewAgent = cognitive co-equal gate with veto; Calibration Agent
  = deferred analyzer role.
- merge = deterministic Dagger function; merge tool excluded from all Agent tool-sets.
- QualityGate ↔ ReviewAgent co-evolve via project-persisted calibration mixins.
- `dagmar-e95b` (quality-gate workflow) inherits the Calibration Agent, post-merge
  watchdog, and two-green flow as design inputs.
