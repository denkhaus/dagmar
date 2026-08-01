# Foundations Review — dagmar (2026-07-31)

Scope: `CONTEXT.md`, `docs/adr/0001-0003`, `docs/agents/*`. Goal: surface
inconsistencies, undefined terms, and gaps before the design solidifies into code.
The project is at an early (documentation-only) stage, so most items below are
expected follow-ups rather than defects. Each item is tagged:

- `[FIX]` — internal contradiction; should be corrected in existing docs now.
- `[GAP]` — concept referenced but not yet defined/decided; needs an ADR or glossary entry.
- `[HOUSE]` — documentation structure / housekeeping.

Mapping to existing seeds is noted where relevant (`dagmar-80dd` wayfinder map,
`dagmar-e95b` quality gate, `dagmar-3684` Go module layout & hex arch,
`dagmar-e795` self-bootstrap & dogfooding).

---

## A. Inconsistencies / contradictions

### A1. `FIX` — "Execution quartet" is both misnamed and self-contradictory
**Location:** `CONTEXT.md` → Key relationships.

Current text:

> **Execution quartet:** `Run = Agent × Sandbox × Workspace`. `Run 1:1 Sandbox`.
> `Task 1:N Runs 1:N Sandboxes`, all under one Engine.

Two problems:
1. **"Quartet" (four) vs. three multiplicands.** The equation names only
   `Agent × Sandbox × Workspace` (three). Counting `Run` itself makes four, but that
   is not stated, so the label reads as a miscount. Either rename (e.g. "execution
   triple/factor") or make the four members explicit: `{Run, Agent, Sandbox, Workspace}`.
2. **Cardinality contradiction (the real bug).** `Run 1:1 Sandbox` is stated, then the
   very next clause `Task 1:N Runs 1:N Sandboxes` parses as `1 Run : N Sandboxes`,
   directly contradicting the `1:1` just asserted. Suggested fix:

   > `Task 1:N Runs`, each `Run 1:1 Sandbox`, all under one Engine.

### A2. `GAP` — Engine cardinality / tenancy is never stated
**Location:** `CONTEXT.md` Tier A (Engine), Tier C (Sandbox), Key relationships.

- Sandbox is "subordinate to the Engine (`Engine ⊃ Sandbox`)".
- "all under **one** Engine" implies a singleton.
- But Projects are N, and os-eco is "N+1 contexts".

It is unclear whether the Engine is one-per-cluster (shared across all Projects), and
how Sandboxes (per-Run, not per-Project) are isolated/quotad across Projects. Add one
explicit line on Engine cardinality and cross-Project isolation. (Tier-A resource, but
dagmar's *deployment* of it is a Tier-C decision → see C1.)

---

## B. Undefined / under-defined terms

### B1. `GAP` — Three "autonomy" concepts with no unifying definition or ADR
**Location:** `CONTEXT.md` L67, L69, L74–75.

| Where | Term |
|-------|------|
| Project CRD | "autonomy level" |
| Agent CRD | "autonomy scope" |
| QualityGate CRD | "autonomy/merge rules … Per-Project / autonomy-level" |

These overlap heavily but no ADR defines (a) the discrete levels, (b) which entity is
authoritative, (c) precedence when they conflict. This is the core of the
"target: fully-autonomous" mandate. → **Needs an ADR (Autonomy model).** (Overlaps
partly with `dagmar-e95b`, but the *policy/level* decision is distinct from the
review-workflow design.)

### B2. `GAP` — "Wayfinder" is referenced but absent from the glossary
**Locations:** `CONTEXT.md` L111 ("Planning is done locally (Wayfinder)"),
`docs/agents/issue-tracker.md` L30–33, wayfinder epic `dagmar-80dd`.

Wayfinder is used as if established, yet the glossary defines neither what it is
(tool? skill? process?) nor its tier. Either add a glossary entry (Tier C, or an
explicit "external process" note) or mark it out-of-scope for the domain model.

### B3. `GAP` — ProjectManifest well-known path and format are never pinned
**Location:** ADR-0003 ("a well-known manifest analogous to `.seeds/` / `.mulch/` /
`.dagger/`"), `CONTEXT.md` (Project "references the repo's ProjectManifest").

The analogy is clear, but the actual path and format (YAML? JSON?) are never stated,
even though the Project CR is said to reference it by "repo + path". Pin a concrete
default (e.g. `.dagmar/project.yaml`) so the Project CR and consumers have a target.

### B4. `GAP` — `Project.checkable-source` vs `ProjectManifest.checkables` is ambiguous
**Location:** `CONTEXT.md` L67 vs ADR-0003.

- Project CR carries **"checkable source"**.
- ProjectManifest carries **"project-specific checkables"**.
- ADR-0003 says the CR "does not duplicate the project-specific content".

So is `Project.checkable-source` merely a pointer to the manifest's checkables, or a
separate dagmar-side default/fallback? ADR-0003's "consequences" claim a unified
checkable chain, but the Project CR's exact role in that chain is not defined. Clarify
the projection (which field is authoritative, and how it flows into
Workspace → CodeWorkspace).

### B5. `GAP` — Prompt pipeline data shapes are undefined
**Location:** `CONTEXT.md` (Prompt CRD), ADR-0003 (prompt-enrichment JSON), Tier A
(`WithPromptFile`).

Three shapes are unnamed: the Prompt **spec**, the ProjectManifest
**prompt-enrichment JSON**, and the **resolved prompt**. Acceptable to defer, but
track as a follow-up (likely lands under prompt/quality-gate work).

---

## C. Referenced-but-missing ADRs

### C1. `GAP` (high priority) — "Hybrid-C" execution topology has no ADR
**Location:** ADR-0002 Context ("execution model Hybrid-C: Go/K8s control plane +
agent pods; Dagger as the hermetic engine").

"Hybrid-C" presupposes A/B/C alternatives, but the control-plane topology decision is
recorded nowhere. This is foundational — it is the *premise* of the CRD-boundary ADR.
→ **Should be its own ADR** (e.g. ADR-0004: Execution topology — Hybrid-C), which
ADR-0002 then references.

### C2. `GAP` — Credentials & secret management: no ADR
**Locations:** `CONTEXT.md` L67 (Project "credentials"), L87 (Sandbox "credentialed"),
Tier B (per-Project os-eco tokens).

For a system targeting autonomy on forks, secret handling is security-critical and
currently unaddressed: storage (K8s Secret?), scoping (per-Project namespace; a
Project must not read another's creds), and injection into Sandbox / `dag.Env()`.
→ **Needs an ADR.**

### C3. `GAP` — Autonomy & QualityGate decision policy: no ADR
**Location:** `CONTEXT.md` Key relationships (gating outcomes
`{auto-merge | revise | escalate | reject}`).

The decision *semantics* (when auto-merge is permitted, what "escalate" means, where
the human-in-the-loop boundary sits) are unspecified. **Reinforces existing seed
`dagmar-e95b`** (Quality gate & multi-agent review workflow); recommend the policy
level be captured as an ADR alongside that work.

### C4. `GAP` — Concurrency / scheduling / cross-Run coordination: no ADR
**Location:** `CONTEXT.md` (Workspace lineage "Run-out → next Run-in" implies
sequential; Run isolation implies possible parallelism).

A singleton Engine, multiple Projects/Agents, and N concurrent Runs raise scheduling,
resource quotas, and coordination questions (are concurrent Runs on one Task allowed?
who sequences Workspace lineage?). Unspecified. → flag for a future ADR.

---

## D. Documentation structure / housekeeping

### D1. `HOUSE` — No `README.md` at the repo root
`CONTEXT.md` (domain vocabulary) and `CLAUDE.md` (agent instructions) exist, but there
is no README — the expected entry point for a project meant to be forked/operated.
Add at least a stub: what dagmar is, current status, how to run (once code exists).

### D2. `HOUSE` — CRD-boundary table is duplicated (`CONTEXT.md` + ADR-0002)
The same six-row table appears in both. Sync/drift risk. Recommend keeping the full
table in **ADR-0002** (the decision record) and a one-line summary + pointer in
`CONTEXT.md`, or explicitly marking one as authoritative.

### D3. `HOUSE` — `Agent 1:N Runs` is missing from "Key relationships"
Agent is "materialized as Runs" and Run is "one execution of an Agent", so the
relationship is implied by the quartet equation but not stated as its own line (cf.
`Project 1:N Tasks`, which *is* listed). Add for completeness.

### D4. `HOUSE` — CRD-field → `Loop` construction mapping is undocumented
Tier A defines `Loop = dag.LLM().WithEnv(env).WithPromptFile(prompt).Loop()`. How the
Agent CRD fields map onto that construction is implied but not written:

| Agent CRD field | Tier-A target |
|-----------------|---------------|
| `model` | `dag.LLM()` |
| `Prompt ref` → canopy resolve | `.WithPromptFile(...)` |
| `tool-set` | `dag.Env()` tools |
| `checkable` | `CodeWorkspace(source, checkable)` |

This is the spec→Dagger bridge; a short section or diagram would help (likely belongs
under `dagmar-3684`, Go module layout & hex arch).

### D5. `HOUSE` — Two label taxonomies coexist without a note
`docs/agents/triage-labels.md` (5 canonical roles) and the wayfinder labels
(`wayfinder:map`, `wayfinder:<type>`) coexist. Their interaction (does a wayfinder
ticket also carry a triage label?) is not documented. Minor.

### D6. `HOUSE` — Two paths into seeds (adapter vs controller) deserve a clarifying note
Tier B defines an `IssueTracker` adapter (→ seeds) for agents, while ADR-0002 says "the
controller observes seeds" directly. These are complementary (agent path vs
controller path) but could read as redundant. One sentence clarifying the split would
help.

---

## E. Implementation maturity (context, not a defect)

- `go.mod` is present but effectively a stub; there is **no application Go source**
  yet (the only `.go` file is a `.claude/skills/...` helper script). This is expected
  at this stage.
- Mulch holds 1 convention (DE/EN language split) + 2 references + 2 meta — i.e. the
  expertise store is just bootstrapping.
- The **dogfooding bootstrap** (ADR-0003: "dagmar itself is a Project") has a mild
  chicken-and-egg: dagmar defines a ProjectManifest and checkables for a repo that has
  no code/checkables yet. Already tracked by `dagmar-e795` (self-bootstrap &
  dogfooding trajectory) — no action beyond keeping that sequencing explicit.

---

## Suggested next ADRs (priority order)

1. **ADR-0004 — Execution topology (Hybrid-C).** Decides the control-plane/agent-pod/
   engine layout that ADR-0002 already assumes. *(C1)*
2. **ADR-0005 — Autonomy model.** Defines autonomy levels and the precedence among
   Project / Agent / QualityGate. *(B1, C3)*
3. **ADR-0006 — Credentials & secret management.** Storage, scoping, injection.
   *(C2)*
4. **ProjectManifest spec v0.** Path, format, and the `checkable-source` projection.
   *(B3, B4)*
5. **Concurrency / scheduling model** (deferred until multi-Run is real). *(C4)*

---

## Already tracked in seeds (no new action — for awareness)

| Finding | Covered by |
|---------|------------|
| Quality-gate review workflow & policy (C3) | `dagmar-e95b` |
| Go module layout / hex arch incl. Loop bridge (D4) | `dagmar-3684` |
| Self-bootstrap & dogfooding trajectory (E) | `dagmar-e795` |
| Overall wayfinder map | `dagmar-80dd` |

Newly surfaced by this review (not yet tracked): **A1, A2, B1, B2, B3, B4, C1, C2, C4,
D1, D2, D3, D5, D6.** Consider filing the highest-value ones (A1 fix, C1, C2, B1) as
seeds.
