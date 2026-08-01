# Foundations Review (repeat) — dagmar (2026-08-01)

Scope: `CONTEXT.md`, `docs/adr/0001-0008`, `docs/agents/*`, `README.md`. Same shape as
`docs/review/2026-07-31-foundations-review.md`. Since that review, ADRs 0004–0008 were
added (execution topology, prompt composition, autonomy model, credentials, engine
tenancy) and a `README.md` appeared; this pass verifies those landed cleanly and surfaces
what remains. Each item is tagged:

- `[FIX]` — internal contradiction; should be corrected in existing docs now.
- `[GAP]` — concept referenced but not yet defined/decided; needs an ADR or glossary entry.
- `[HOUSE]` — documentation structure / housekeeping.

Open seeds (from `sd list`): `dagmar-80dd` (wayfinder map), `dagmar-e95b` (quality-gate
workflow), `dagmar-3684` (Go module layout / hex arch), `dagmar-e795` (self-bootstrap).
Resolved-in seeds referenced by the new ADRs (`dagmar-4271`, `-8097`, `-fa45`, `-cbb8`,
`-4c9f`) all exist (closed), so ADR ↔ seed back-links are intact.

> **Resolved since 2026-07-31** (for traceability — these are *not* defects): prior A1
> (quartet miscount + `1:1` contradiction) fixed in `CONTEXT.md`; A2 (Engine cardinality)
> decided singleton in ADR-0008; B1 (three autonomy concepts) resolved by ADR-0006; B2
> (Wayfinder) glossary entry added; B3 (manifest path/format) pinned
> `.dagmar/project.yaml`; B4 (`checkable-source` vs manifest) — CR has no
> `checkable-source` field; B5 (prompt shapes) substantially resolved by ADR-0005; C1
> (Hybrid-C) → ADR-0004; C2 (credentials) → ADR-0007; C3 (autonomy/gate policy) →
> ADR-0006; D1 (README) exists; D2 (CRD-table duplication) collapsed to a pointer; D3
> (`Agent 1:N Runs`) added; D4 (CRD→Loop bridge) table added; D6 (two paths to seeds)
> clarifying line added.

---

## A. Inconsistencies / contradictions

### A1. `FIX` — ADR-0007 makes the per-Project namespace the hard trust boundary; ADR-0008 makes namespaces optional
**Location:** `docs/adr/0007-credentials-secret-management.md` → Decision §1; `docs/adr/0008-engine-tenancy.md` → Decision §4; `CONTEXT.md` L196.

- ADR-0007 §1: "*Each `Project` reconciles into a dedicated namespace. … The namespace is
  the hard, auditable trust boundary.*" Its enforcement layer 1 is "*Kubernetes RBAC —
  dagmar's per-Project service accounts (and the controller's secret-read) are
  **namespace-scoped**; there is **no grant to read Secrets in another Project's
  namespace**.*"
- ADR-0008 §4: "*Namespaces = optional (defense-in-depth). Namespaces are **not required**
  for tenancy … (This sharpens ADR-0007: credential isolation is a Sandbox/encapsulation
  property; namespaces are tenancy hygiene, **not the credential control**.)*"

These cannot both be true. ADR-0007 grounds credential isolation entirely in the
per-Project namespace (RBAC scoped to "another Project's namespace"). If a deployment
omits per-Project namespaces — which ADR-0008 explicitly permits — that RBAC control has
**no per-Project namespace to bind to**, so ADR-0007's "hard boundary" dissolves.
ADR-0008's "sharpens" note reassigns the boundary to "Sandbox/encapsulation", but ADR-0007
**never establishes the Sandbox as a credential-isolation control** (the Sandbox is a
runtime pod + engine-session; the only related property ADR-0007 states is the
controller-discipline invariant, which is a *controller* property, not a Sandbox one).

Concrete undefined created by the clash: in a namespace-less deployment, **where do the
per-Project Secrets physically live?** ADR-0007 §1 places them "in the dedicated
namespace" — which may not exist. `CONTEXT.md` L196 (Architectural decisions) echoes the
mandatory framing ("ADR-0007 — … **per-Project namespace** …"), contradicting ADR-0008.

**Recommend:** pick one. (a) Namespaces are **mandatory** for Projects — ADR-0007 wins;
ADR-0008 §4 drops "optional" and reframes its RBAC/cache isolation as
*orthogonal-to-namespaces*, not *instead-of*. Or (b) namespaces are **optional** and
ADR-0007 is amended to ground credential isolation on a namespace-independent mechanism
(controller-discipline + admission policy / a non-namespace RBAC model) and the
"hard-boundary = namespace" claim is retracted. File as an amendment to ADR-0007/0008 or
a superseding ADR-0009. **Security-critical; highest priority.**

### A2. `FIX` — `CONTEXT.md` Engine glossary still calls cardinality "an open execution decision; see ADR-0004", but ADR-0008 decided singleton
**Location:** `CONTEXT.md` L33 (Engine glossary parenthetical) vs ADR-0008 §1; cf.
`CONTEXT.md` → Open questions.

- `CONTEXT.md` L33: "_(Engine cardinality/tenancy — singleton vs per-Project,
  cross-Project isolation — is an **open** execution decision; see **ADR-0004**.)_"
- ADR-0008 §1: "*Engine cardinality = **SINGLETON** … decided.*"
- `CONTEXT.md` is internally split: its own *Open questions* section already records
  "_engine tenancy decided in **ADR-0008**, seeds `dagmar-cbb8`_", contradicting the
  glossary's "open / ADR-0004" parenthetical ~90 lines earlier.

**Fix:** update the Engine glossary parenthetical to "_decided: singleton engine
(ADR-0008)_" and point at ADR-0008, not ADR-0004.

### A3. `FIX` (minor) — ReviewAgent prompt composition lists 2 operands in CONTEXT.md, 3 in ADR-0006
**Location:** `CONTEXT.md` → Roles (ReviewAgent) vs `docs/adr/0006-autonomy-model.md` → Calibration Agent.

- `CONTEXT.md`: "_Prompt = dagmar `review-agent` mixin ⊕ project `review-calibration`
  mixin (ADR-0005)._"
- ADR-0006: "_Composed ReviewAgent prompt = dagmar `review-agent` ⊕ project
  `review-calibration` ⊕ **project content**._"

ADR-0006's third operand (project content) is dropped in `CONTEXT.md`. Reconcile the
wording (add "⊕ project content" to the glossary line, or drop it from ADR-0006 if
intentional). Minor, but it is exactly the kind of prompt-composition drift ADR-0005
exists to prevent.

---

## B. Undefined / under-defined terms

### B1. `GAP` — "per-Project cache-volume name" isolation is asserted by design but not empirically validated (deferred)
**Location:** `docs/adr/0008-engine-tenancy.md` → Decision §3 + Consequences.

ADR-0008 §3: "*Dagger isolates cache by volume name. The controller allocates distinct
cache-volume names per Project, which gives cross-Project cache isolation (Research Q1).
Cache poisoning across Projects is prevented **as long as Projects never share a
cache-volume name**.*" Its Consequences then: "_Deferred: … an empirical Q1
cache-poisoning test (the design conclusion stands …)._"

The design's entire cross-Project cache-safety rests on an **unverified assumption**
about Dagger's cache-volume-name isolation. It is tracked and explicitly deferred, so not
a defect — but it is the one cross-Project safety claim with no evidence behind it. Flag
for validation **before** multi-Project autonomy (especially `mergeAuthority == auto`) is
enabled. Low urgency; appropriately deferred.

---

## C. Referenced-but-missing ADRs

### C1. `GAP` — Concurrency / scheduling / Workspace-lineage sequencing: still no ADR (deferred, unchanged)
**Location:** `docs/adr/0008-engine-tenancy.md` → Decision §5; `CONTEXT.md` → Open questions.

ADR-0008 §5: "_Whether concurrent Runs on one Task are allowed, and who sequences
Workspace lineage, are **control-plane** concerns … deferred to the controller design._"
`CONTEXT.md` Open questions still lists the same item. This is the prior review's **C4,
unchanged** — explicitly deferred, not newly surfaced. It will land as a control-plane
ADR once the controller is designed.

No genuinely **new** referenced-but-missing ADR surfaced: the ADR set 0001–0008 now covers
every concept `CONTEXT.md` assumes. The namespace clash (A1) is an **amendment** need, not
a missing ADR.

---

## D. Documentation structure / housekeeping

### D1. `HOUSE` — ADR-0007 and ADR-0008 cross-reference each other without a reconciliation pointer
**Location:** ADR-0007 Consequences ("ADR-0004 inherits: the per-Project namespace
topology …") vs ADR-0008 §4/Consequences ("This sharpens ADR-0007 …").

Each ADR reads as if the other conforms, but the namespace treatment clashes (A1). At
minimum, add a forward/back cross-ADR pointer in each so a reader of either sees the open
question. Resolves automatically once A1 is decided.

### D2. `HOUSE` (minor, unchanged) — triage labels vs wayfinder labels interaction still undocumented
**Location:** `docs/agents/triage-labels.md` (5 canonical roles) vs `docs/agents/issue-tracker.md`
(wayfinder labels `wayfinder:<type>`).

Whether a wayfinder child ticket also carries a triage role is not stated. Low value;
carried from the prior review (D5 there).

---

## E. Implementation maturity (context, not a defect)

- `README.md` now exists (24-line stub: Status / Read first / Run) — prior D1 resolved.
- `go.mod` is a stub (`go 1.26.1`, no `require`s); the 5 `.go` files are all `.dagger/`
  generated/main + one `.claude/skills` helper. Still **no application code** — expected
  at the documentation-only stage.
- `docs/research/` now holds three ADR-evidencing spikes (`canopy-prompt-model.md`,
  `dagger-in-k8s.md`, `spike-engine-tenancy.md` + a `q3-client.yaml`); all three ADR
  references resolve. The new ADRs are evidence-backed — a marked maturity gain.
- Mulch grew to 3 conventions / 4 references / 11 meta (was 1 / 2 / 2).

---

## Suggested next ADRs (priority order)

1. **Reconcile ADR-0007 ↔ ADR-0008 on the namespace / credential boundary (highest
   priority — security-critical).** Decide mandatory vs optional namespaces and re-ground
   credential isolation accordingly. *(A1, D1)* Form: amendment to ADR-0007/0008, or a
   superseding ADR-0009. Until this is settled the system's credential-isolation boundary
   is self-contradictory between its two newest ADRs.
2. **Concurrency / scheduling / Workspace-lineage sequencing (control-plane ADR).**
   *(C1)* Deferred until the controller is designed; ADR-0008 §5 explicitly parks it
   here. Not blocking while the system is documentation-only.
3. *(Validation, not an ADR — low urgency)* Empirically confirm the cache-volume-name
   isolation assumption *(B1)* before multi-Project autonomy is enabled.

---

## Already tracked in seeds (no new action — for awareness)

| Finding | Covered by |
|---------|------------|
| Concurrency / scheduling / Workspace lineage *(C1, deferred)* | `dagmar-cbb8` (resolved engine-tenancy; concurrency explicitly deferred to controller design) |
| Calibration Agent workflow + two-green/veto flow | `dagmar-e95b` |
| Go module layout / CRD→Loop bridge | `dagmar-3684` |
| Self-bootstrap & dogfooding trajectory | `dagmar-e795` |
| Overall wayfinder map | `dagmar-80dd` |

Resolved-in back-references (all closed/existing): ADR-0001/0002/0003 → `dagmar-4271`;
ADR-0005 → `dagmar-8097`; ADR-0006 → `dagmar-fa45`; ADR-0008 → `dagmar-cbb8`; ADR-0007 →
`dagmar-4c9f`.

**Newly surfaced by this review (not yet tracked):** **A1, A2, A3, B1.** (D1 is a
consequence of A1; D2 is unchanged.) The single highest-value item to plan against is
**A1** — it is security-critical and leaves the credential-isolation boundary
self-contradictory between the two newest ADRs.
