# Mache-Review — Resolution & Session Handoff (2026-08-01)

> Companion to `docs/review/04-2026-08-01-8e8d35d-mache.md` (the findings). This file records
> what the 2026-08-01 resolution pass changed and hands off the open design work. Read this
> first when resuming.

## TL;DR

- Doc-consistency findings **A1, B1, B2, B3, D1, D2 → resolved** in the source docs
  (committed in the mache-review resolution commit; working tree is clean).
- **ADR-0010** (Sandbox trust-zones) and **ADR-0011** (general Workflow-CRD framework) →
  **deferred** by the user on 2026-08-01. ADR-0010 grilling is explicitly postponed; §3
  below captures its decision space so the next session can grill informed.
- **C3** (concurrency/scheduling) and **D3** (triage↔wayfinder labels) → deferred per the
  review. **D1** full mission-statement reframe → seed `dagmar-1775`.

---

## 1. Resolved this pass (doc edits — uncommitted)

| Item | Tag | Fix | Files |
|------|-----|-----|-------|
| **A1** | FIX | Gate timing reconciled to ADR-0009's model: the gate is **invariant, always pre-merge** (advisory under `authority=human`); the post-merge behavior is a **separate watchdog workflow** with a filtered trigger, *not* "the gate". Fixed ADR-0009 §7's loose `(ADR-0006)` pre-merge citation. | `CONTEXT.md` (Gating flow), `docs/adr/0006-autonomy-model.md` (reactive tier), `docs/adr/0009-…` §7 |
| **B1** | GAP | **Workflow** glossary stub added (forthcoming CRD → `dagmar-ff60`; today realized via `Run`/`QualityGate`/`Trigger`) + note in the CRD-boundary section. | `CONTEXT.md` |
| **B2** | GAP | Pinned the relationship: **manifest = *what* the checkables are, `dagmar-gate` = *how* to run them** (no projection ambiguity — manifest stays authoritative). Recorded `just` (Justfile) as a **mandatory conformance dependency** introduced by ADR-0009, not ADR-0003. | `docs/adr/0009-…` §2 + Consequences, `CONTEXT.md` (ProjectManifest) |
| **B3** | GAP | ADR-0009 §3 "excludes any network-capable tool" reframed as the **principle**, not a pinned surface; the per-tool boundary is deferred to the forthcoming Sandbox trust-zone ADR (forward-pointer names it). | `docs/adr/0009-…` §3 |
| **D1** | HOUSE | Opening forward-pointer to the "Dagger-based software factory" reframe (full mission-statement rewrite waits for `dagmar-1775`). | `CONTEXT.md` L1 |
| **D2** | HOUSE | `stufenweise` → `incrementally` (stray German in an English ADR). | `docs/adr/0009-…` §5 |

Mulch: the A1 reconciliation decision is recorded under **mx-d39154** (merged into the
ADR-0009 decision record — same decision family).

## 2. Committed state

- Branch `main`, **committed** (mache-review resolution commit). The commit bundles: the
  3 source-doc edits (`CONTEXT.md`, `docs/adr/0006-autonomy-model.md`,
  `docs/adr/0009-quality-gate-workflow.md`; +54/−14), this handoff, the mulch decision
  record (**mx-d39154**, committed with the docs per the `7f60779` precedent), and the
  review-agent's rename of the review files to the `NN-YYYY-MM-DD-shorthash-label` scheme
  + reference cleanup.
- No seed changes this pass — run `sd sync` only if seeds were touched elsewhere.

## 3. Deferred — ADR-0010: Sandbox trust-zones / hermetic-LLM network segmentation  `[HIGH, security]`

- **Owner seed:** `dagmar-911b` (open). **User deferred grilling on 2026-08-01.**
- **Why it can't stay dark long:** ADR-0007's *second defense-in-depth layer* (Sandbox
  encapsulation) **and** ADR-0009 §3 (hermetic LLM via tool-set) both assume this boundary.
  It is the prompt-injection → exfiltration defense. Until it exists, "hermetic" is an
  asserted principle, not an enforced surface (see B3 above).
- **Decision space to grill:**
  1. **Trust zones** — which zones exist? (Candidates surfaced by ADR-0009: hermetic-LLM
     [no network], deterministic-gate [`dagmar-gate`, has network], bootstrap
     [`dagmar-bootstrap`, has network].)
  2. **Per-zone egress / tool surface** — concretely: is `container` withheld for hermetic
     agents? `git` (remotes)? Or only `http`? The `CONTEXT.md` `Tool` glossary lists
     `dag.git` / `container` / `http`; `container` and `git` are network-capable, so
     ADR-0009's "excludes any network-capable tool" is genuinely ambiguous today.
  3. **Enforcement mechanism** — tool-set exclusion alone, a Sandbox-pod `NetworkPolicy`
     alone, or **both** (defense-in-depth)? What *actually* guarantees "no network" for the
     LLM loop, independent of the LLM's own tool-set?
- **To resume:** invoke `/grilling` on ADR-0010. On decision, author
  `docs/adr/0010-sandbox-trust-zones.md`, then **update `CONTEXT.md` `Tool` glossary and
  ADR-0009 §3 to cite it** — the B3 forward-pointer already names this ADR, so the link is
  one edit away.

## 4. Deferred — ADR-0011: general Workflow-CRD framework  `[LOW]`

- **Owner seed:** `dagmar-ff60` (open). The quality-gate family is operable today via
  `Run`/`QualityGate`/`Trigger`; the Workflow glossary stub (B1) bridges the conceptual gap.
- Unblock when the software-factory reframe (D1 / `dagmar-1775`) is prioritized — ADR-0011
  finalizes the `Workflow` CRD and lets D1's mission statement be rewritten in full.

## 5. Pointers

- **This review cycle:** findings `docs/review/04-2026-08-01-8e8d35d-mache.md` · handoff `docs/review/04-2026-08-01-8e8d35d-mache-handoff.md` (this file)
- **Prior cycle:** `docs/review/03-2026-08-01-5867bcb-review.md` (resolved by commit `7f60779`)
- **Seeds:** `dagmar-911b` (trust-zones) · `dagmar-ff60` (workflow CRD) · `dagmar-1775`
  (software-factory reframe / D1) · `dagmar-80dd` (wayfinder map) · `dagmar-e795` (self-bootstrap)
- **ADRs touched this pass:** 0006, 0009 · **dependent:** 0007
- **Mulch:** mx-d39154 (ADR-0009 + A1 reconciliation) · mx-36fafa (namespace two-layer defense)

## 6. Suggested next steps (priority)

1. ~~Commit the doc-consistency fixes~~ — **done** (the mache-review resolution commit). Run `sd sync` before pushing only if seeds changed.
2. When ready: `/grilling` → **ADR-0010** using the §3 decision space.
3. After ADR-0010 lands: update `CONTEXT.md` `Tool` glossary + ADR-0009 §3 citations.
4. Parked: ADR-0011 (`dagmar-ff60`), D1 full reframe (`dagmar-1775`), concurrency (C3).
