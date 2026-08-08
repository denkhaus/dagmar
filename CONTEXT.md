# dagmar — domain model & ubiquitous language

dagmar is a (target: fully-autonomous) Dagger/Kubernetes-hybrid multi-agent coding
system, written in Go, that works on the owner's own repositories and their forks.
Strategic destination (recorded in ADR-0009, tracked in seed `dagmar-1775`): a
Dagger-based **software factory** organized around a Workflow concept.
This document is the single source of truth for dagmar's domain vocabulary: every
issue, design, and code symbol uses these terms as defined here.

> Authoritative for the domain model resolved in seeds **dagmar-4271**. Spoken
> language is German; all persisted writing (this file, issues, code, comments) is
> English.

## The layered language model

dagmar's ubiquitous language has three tiers. **Terms are coined only in Tier C.**
Tier A is reused by reference; Tier B (Project Hook Services) are Dagger functions in the
Project module (ADR-0018: ~~consumed behind adapter ports~~ — the Go port layer is removed).

| Tier | Origin | Treatment |
|------|--------|-----------|
| **A — Dagger** | Dagger primitives | reused by reference; names never re-coined |
| **B — Project Hook Services** | seeds / mulch / canopy | Dagger functions in the Project module, discovered by name, verified by introspection (ADR-0018) |
| **C — dagmar core** | dagmar's own | where we coin terms |

**Rule:** never coin a Tier-C term for something Dagger already names (Tier A). If a
concept is Dagger's, reference it; if it is a Project Hook Service, expose it as a Dagger
function in the Project module. See ADR-0001; ADR-0018 (Tier-B redefined).

## Glossary

### Tier A — Dagger (reused, not re-coined)

- **Engine** — the hermetic Dagger execution engine. The resource that provisions and
  contains Sandboxes (`Engine ⊃ Sandbox`). dagmar runs an in-cluster instance but
  treats the Engine as a Tier-A resource. _(Engine cardinality/tenancy is decided: a
  singleton engine serves all Projects; cross-Project isolation via per-Project
  ServiceAccount/RBAC + cache-volume names — see ADR-0008.)_
- **LLM** — Dagger's LLM primitive (`dag.LLM()`); the cognition provider. dagmar does
  not reimplement cognition.
- **Env** — Dagger environment bundling inputs/outputs/tools for an LLM (`dag.Env()`).
- **Workspace** — the project source as a `*dagger.Directory`, passed to the agent's
  `Env` via `env.WithWorkspace(source)`. The checkable (in-loop self-verification) is
  `env.Checks()` — module-annotated check functions discovered by the Env, not a constructor
  argument. Post-Loop changes captured via `workspace.Update()` → `*Changeset`.
  (ADR-0020: `CodeWorkspace(source, checkable)` was a pre-API conceptual design; v0.21.8 uses
  `Env.WithWorkspace` + `Env.Checks`.)
- **checkable** — the project's mechanical self-verification (build/test/lint), defined
  per-project **in code** inside the `dagmar-gate` Dagger function (ADR-0017 §3; formerly
  manifest-declared per ADR-0003, superseded). In v0.21.8, in-loop self-verification runs via
  `env.Checks()` → `*CheckGroup` — module-annotated check functions discovered by the Env
  (ADR-0020). Reused both in-loop (agent self-verifies while working) and as the mechanical
  layer of the QualityGate.
- **Loop** — `dag.LLM(opts).WithEnv(env).WithPromptFile(prompt).Loop()`; the agent
  cognition loop (v0.21.8: `LLMOpts{Model, MaxAPICalls}`). A Run drives exactly one Loop.
- **TokenUsage** — Dagger's cost observability (`agent.TokenUsage()`).
- **Tool** — Dagger configuration: what an agent may call (`dag.git` / `container` /
  `http` plus Project Hook Service exposures). dagmar coins no Tool type; an Agent's
  permitted tools are its `tool-set` field. A **hermetic** agent = its `tool-set` carries no
  network-capable tool (`http`, `git` remote, the whole `container` tool) — a *tool-surface*
  constraint, **not** a network air-gap. See ADR-0011 §2.

### Tier B — Project Hook Services (Dagger functions in the Project module)

All three are bound **per-Project** (N+1 contexts: dagmar-own + each target Project —
dogfooding).

- **IssueTracker** (→ seeds) — create/read/update/close issues (= Tasks); manage
  dependencies and plans. Canonical work handle.
- **Memory** (→ mulch) — read/write project expertise (conventions, patterns,
  failures, decisions); per-Project recall.
- **Prompts** (→ canopy) — dagmar composes an Agent's prompt by cross-store merging
  (ADR-0005): dagmar operational mixins (dagmar `.canopy/`) ⊕ project-content prompts
  (project `.canopy/`); emits the resolved `.md` for `WithPromptFile`.

### Tier C — dagmar core

**CRDs (declarative: definitions / policy / registration / observable execution):**

> **CRD set** (ADR-0002, extended by ADR-0016): `{Project, Agent, Prompt, QualityGate, Trigger, Workflow, Run}`.

- **Project** (CRD) — a registered, repo-backed repository dagmar operates on (own
  repo or fork). Carries **dagmar-operational config only** (Project Hook binding,
  credentials (the three typed classes `vcs`/`hook-service`/`llm`; ADR-0007), and the
  **autonomy setting** — `merge-authority` (human|auto),
  `trigger-tier` (on-demand|reactive|proactive); ADR-0006 — and references the repo's
  Project module (`.dagmar/`, the Dagger conformance module). Project-specific content
  (checkable logic, Project Hook implementations) lives in the project module's code,
  not on the CR.
- **Agent** (CRD) — a durable role/persona (coder, reviewer, researcher, …): model +
  Prompt ref + tool-set + checkable + role bounds. Materialized as Runs. Agents have
  **no merge authority** — merge is a deterministic controller function (ADR-0006); the
  merge tool is in no Agent's tool-set.
- **Prompt** (CRD) — a **reference to canopy prompts**, not a dagmar-invented spec: a
  project-content prompt (in the project's `.canopy/`) plus dagmar operational mixins
  (output-format / review-gating / safety / tool-rules, in dagmar's own canopy). dagmar
  composes them at run time (Variant A, ADR-0005) into the final prompt passed to
  `dag.LLM().WithPromptFile(...)`.
- **QualityGate** (CRD) — the **deterministic** layer deciding whether a candidate may
  advance (checkables + rules). **Invariant** — always secures quality. Merge requires
  QualityGate.green **AND** ReviewAgent.approve (two green lights, ADR-0006); the
  ReviewAgent holds a hard veto. Per-Project; codebase-evolving (grows via the
  Calibration Agent).
- **Trigger** (CRD) — declarative event source that creates Tasks. Reactive (GitHub
  webhooks) or proactive (cron housekeeping). Bound to a Project + Workflow
  (`spec.workflowRef`, ADR-0016 §6) + event-mapping.
- **Project Hook** — a Dagger module function the Project exposes as a conformance entry
  point. All Project Hooks are Go functions in the Project's `.dagmar/` Dagger module (ADR-0017).
  Two categories: **Programmatic hooks** (`dagmar-bootstrap`/`dagmar-gate`, called by dagmar's
  controller, take `source *dagger.Directory` + `githubToken *dagger.Secret`) and **LLM-Tool hooks**
  (`dagmar-issues`/`dagmar-memory`/`dagmar-prompt`, exposed as tools on the agent's `Env`, take
  only operation-specific params — workspace accessed implicitly via `dag.CurrentModule()`).
  Signatures + conformance check specified in ADR-0019. Vendor-agnostic: the function name is the
  contract, not the backing service. LLM-Tool hooks are mandatory when an LLM agent is involved
  (noop-allowed). `dagmar-prompt` supplements (not replaces) ADR-0005's cross-store merge.
- **Workflow** (CRD) — a pipeline template referencing Dagger Go functions plus
  controller-interpreted orchestration metadata. **Not** a step DSL — the pipeline form
  (e.g., gate → review → merge with revise loop) is hardcoded in the controller per
  workflow type; the Workflow carries the function-independent metadata (`qualityGateRef`,
  `agents` per role, `maxReviseRounds`, `requiresTwoGreen`). The quality-gate family
  (ADR-0009) is the first instance. Workflow has no Status — it is a pure definition
  (template); orchestration state lives on the orchestration Run. Decided in ADR-0016.
- **Run** (CRD) — one execution: either **atomic** (one Agent, one Sandbox, one Workspace —
  one Dagger function call) or **orchestration** (supervises N atomic Sub-Runs per a Workflow
  template; no Agent/Sandbox/Workspace of its own). Dual-mode decided in ADR-0016 §2. The
  observable, reconciled execution unit; carries status, token usage, outcome.

**Non-CR entities (canonical elsewhere or runtime artifacts):**

- **Task** — a unit of work on exactly one Project; ≡ one seeds issue (1:1, canonical).
  Lifecycle: created → Runs → resolved. Spawns 1..N Runs.
- **Sandbox** — an isolated execution slot subordinate to the Engine
  (`Engine ⊃ Sandbox`); the credentialed (per-Run projected secret subset; ADR-0007),
  resource-bounded pod + engine-session an Agent process runs in. `1 Run : 1 Sandbox`.
- **Workspace** — a task-scoped, Run-isolated clone of a Project on a branch
  (`dagmar/<run-name>`); handed to the agent's Env as a `*dagger.Directory` via
  `env.WithWorkspace(source)`. Ephemeral (engine git-fetch cache, no persistent volume,
  ADR-0020 D1). Strict isolation per Run (no shared clone); lineage via git branches
  (Run-out → next Run-in from branch head). Post-Loop: `workspace.Update()` → `*Changeset`
  → controller pushes branch + creates PR (agents never hold push authority, ADR-0006/0007).

**In-repo manifest (not a CRD):**

- **ProjectManifest** — the in-repo file (`.dagmar/project.yaml`) that each Project exposes.
  **ADR-0017 supersedes the `checkables:` section** — checkable definitions now live in the
  `dagmar-gate` Go function, not in the manifest. The manifest is **slimmed, not removed** (ADR-0017
  §4): it carries Project metadata (display name, description, version) dagmar needs at runtime.
  Git-native, versioned with the code; the Project CR references it by repo + path.

**Roles (Agent specializations, not separate types):**

- **ReviewAgent** — an Agent role that cognitively reviews another Run's output and
  holds a **hard veto**; co-equal gate with the QualityGate (merge needs both green,
  ADR-0006). Prompt = dagmar `review-agent` mixin ⊕ project `review-calibration` mixin ⊕
  project content (ADR-0005).
- **Calibration Agent** *(deferred)* — a non-gating LLM step that, on QualityGate ↔
  ReviewAgent disagreement, diagnoses the cause and emits project-specific
  `review-calibration` mixins into the project's canopy (ADR-0006).
- **planner / architect** — an Agent role that decomposes work into seeds issues
  (future).

**Explicitly not dagmar types:**

- **Plan** — not a Tier-C type (current). A seeds / IssueTracker concept. Planning is
  done locally (Wayfinder), translated into seeds issues, then agents work the issues.
  (Future direction — human-facing Plan-Agent via a chat interface — recorded in mulch.)

**External concepts (referenced, not part of the runtime model):**

- **Wayfinder** — the human planning process used to chart dagmar's direction early on
  (e.g. the `dagmar-80dd` wayfinder map in seeds). Planning with Wayfinder produces
  seeds issues that dagmar agents then work. Not a dagmar Tier-C type.

## Key relationships

- **Execution quartet (atomic Runs/Sub-Runs):** an atomic Run is the product of
  `{Agent, Sandbox, Workspace}` — Run itself is the fourth member. `Agent 1:N Runs` (a role is
  materialized as many Runs); `Task 1:N Runs`, each atomic `Run 1:1 Sandbox`, all under one
  Engine. **Orchestration Runs** (ADR-0016 §2) supervise N atomic Sub-Runs per a Workflow
  template and have no Agent/Sandbox/Workspace of their own — the quartet applies to atomic
  Runs and Sub-Runs.
- **Work hierarchy:** `Project 1:N Tasks`; `Task ≡ 1 seeds issue`; `Task 1:N Runs`.
- **Workspace:** per-Task base ref; per-Run isolated clone with in/out lineage.
- **Gating flow:** coder-Run → candidate → **gate (always pre-merge)** → **two green
  lights** (QualityGate ∧ ReviewAgent.approve) → `merge ⟺ both green ∧ authority==auto`;
  else `{revise | escalate}`. The gate is **invariant** — it always runs on the candidate
  **pre-merge**; under `authority=human` the Gate + Review run pre-merge as **advisory**
  and the human merges (ADR-0006, ADR-0009 §7). Merge is a deterministic Dagger function,
  not an LLM action (ADR-0006). The **post-merge watchdog** is a **separate workflow**
  with a filtered trigger (fires only on human/spontaneous merges); Gate-Red/Veto there
  opens a fix-PR (ADR-0009 §8) — it is *not* "the gate running post-merge".
- **Disagreement (Gate ↔ Reviewer):** revise if the veto is actionable; else
  **escalate** to a human. The deferred Calibration Agent (ADR-0006) will later
  diagnose gate-gaps automatically; until then every unresolved disagreement
  escalates.
- **Trigger flow:** Trigger → seeds issue → Task → Run(s).
- **Project Hook Services:** per-Project Dagger functions; N+1 contexts (ADR-0018).
- **Two paths to seeds:** the controller observes seeds directly (scheduling/state);
  agents use the IssueTracker adapter (CRUD issues). Complementary, not redundant.

## CRD boundary

CRDs: `{Project, Agent, Prompt, QualityGate, Trigger, Workflow, Run}` (Workflow added by
ADR-0016). Non-CR: `{Task, Sandbox, Workspace, ProjectManifest}`. The boundary follows each
entity's state property (declarative/reconciled → CR; canonical-elsewhere or runtime → not) —
see **ADR-0002** for the full table and rationale (extended by ADR-0016 §7).

## CRD fields → Dagger construction (bridge)

How an `Agent`'s CRD fields project onto the Tier-A `Loop` construction (detailed under
seeds `dagmar-3684`, Go module layout & hex arch):

| Agent CRD field | Tier-A target |
|-----------------|---------------|
| `model` | `dag.LLM(LLMOpts{Model})` |
| `prompt` ref → canopy resolve | `.WithPromptFile(...)` |
| `tool-set` | tools on `dag.Env()` |
| `checkable` | `env.Checks()` → `*CheckGroup` (v0.21.8; ADR-0020) |

## Open questions (tracked, not yet decided)

## Architectural decisions

See `docs/adr/`:

- **ADR-0001** — Layered ubiquitous language (Tier A/B/C)
- **ADR-0002** — Kubernetes CRD boundary
- **ADR-0003** — Project conformance via in-repo ProjectManifest
- **ADR-0004** — Execution topology (Hybrid-C)
- **ADR-0005** — Prompt composition (dagmar-side cross-store merge, Variant A)
- **ADR-0006** — Autonomy model (slim axes, deterministic merge, two-green + veto)
- **ADR-0007** — Credentials & secret management (per-Project namespace, typed secrets, ESO, projected injection)
- **ADR-0008** — Engine tenancy & Run concurrency (singleton engine, kube-pod:// agent pods + RBAC, per-Project cache volumes)
- **ADR-0009** — Quality-gate workflow family (deterministic Dagger-function gate, hermetic LLM via tool-set, gate-before-review, Calibration loop, post-merge watchdog)
- **ADR-0010** — Go module layout & hexagonal architecture (logic in `.dagger/internal`; functional core; `Dagmar` main object + `New` constructor)
- **ADR-0011** — Sandbox trust-zones (hermetic LLM via tailored tool-sets; calculated residual risk; one singleton engine unchanged)
- **ADR-0012** — Self-bootstrap & dogfooding trajectory (kind substrate; dispatch-vertical-first; lean controller-runtime; always-Dagger gate-family `dagmar-bootstrap`/`dagmar-gate`; cognition-before-autonomy)
- **ADR-0013** — Kubernetes control-plane design (topology, reconciliation & dispatch)
- **ADR-0014** — Platform/project scope separation (platform module vs. project conformance module)
- **ADR-0015** — Per-Project-scoped identity (SA, RBAC, cache-vol isolation)
- **ADR-0016** — Workflow-CRD framework (pipeline templates, dual-mode Run, controller-driven orchestration)
- **ADR-0017** — Unified Project Hooks (everything is Dagger code; checkables move into dagmar-gate; LLM-Tool hooks)
- **ADR-0018** — Go port/adapter layer removed; Project Hook Services are Dagger functions; Tracer/Span sole surviving port
- **ADR-0019** — Project Hook function signatures + introspection conformance; dagmar-prompt supplements (not replaces) ADR-0005 merge
- **ADR-0020** — Workspace & repository interaction model; ephemeral Directory clones, branch-based lineage, controller-side PR, env.Checks() as checkable
